package service

import (
	"context"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"torrentguard/internal/arr"
	"torrentguard/internal/classifier"
	"torrentguard/internal/config"
	"torrentguard/internal/qbit"
	"torrentguard/internal/state"
)

type QBit interface {
	Login(context.Context) error
	Torrents(context.Context) ([]qbit.Torrent, error)
	Files(context.Context, string) ([]qbit.File, error)
	Tag(context.Context, string, string) error
	Pause(context.Context, string) error
	Delete(context.Context, string, bool) error
}

type Reporter interface {
	ReportFailed(context.Context, string) (bool, error)
}

type Metrics struct {
	TorrentsScanned, MetadataUnavailable, DangerousDetected, SuspiciousDetected atomic.Uint64
	QBitActionsTaken, SonarrReportsOK, SonarrReportsFailed                      atomic.Uint64
	RadarrReportsOK, RadarrReportsFailed                                        atomic.Uint64
}

func (m *Metrics) Snapshot() map[string]uint64 {
	return map[string]uint64{
		"torrents_scanned": m.TorrentsScanned.Load(), "metadata_unavailable": m.MetadataUnavailable.Load(),
		"dangerous_detected": m.DangerousDetected.Load(), "suspicious_detected": m.SuspiciousDetected.Load(),
		"qbit_actions_taken": m.QBitActionsTaken.Load(), "sonarr_reports_ok": m.SonarrReportsOK.Load(),
		"sonarr_reports_failed": m.SonarrReportsFailed.Load(), "radarr_reports_ok": m.RadarrReportsOK.Load(),
		"radarr_reports_failed": m.RadarrReportsFailed.Load(),
	}
}

type Service struct {
	Config config.Config
	QBit   QBit
	Store  *state.Store
	Sonarr Reporter
	Radarr Reporter
	Log    *slog.Logger
	M      *Metrics
}

func New(cfg config.Config, qb QBit, store *state.Store, logger *slog.Logger) *Service {
	return &Service{Config: cfg, QBit: qb, Store: store, Sonarr: arr.New(cfg.SonarrURL, cfg.SonarrAPIKey),
		Radarr: arr.New(cfg.RadarrURL, cfg.RadarrAPIKey), Log: logger, M: &Metrics{}}
}

func (s *Service) Scan(ctx context.Context) error {
	torrents, err := s.QBit.Torrents(ctx)
	if err != nil {
		return err
	}
	present := make(map[string]struct{}, len(torrents))
	for _, torrent := range torrents {
		present[torrent.Hash] = struct{}{}
	}
	if err := s.Store.DeleteMissing(ctx, present); err != nil {
		return err
	}
	sort.SliceStable(torrents, func(i, j int) bool {
		return s.target(torrents[i]) != "none" && s.target(torrents[j]) == "none"
	})
	for _, torrent := range torrents {
		torrentCtx, cancel := context.WithTimeout(ctx, 7*time.Second)
		err := s.scanTorrent(torrentCtx, torrent)
		cancel()
		if err != nil {
			s.Log.Error("torrent scan failed", "hash", torrent.Hash, "name", torrent.Name, "error", err)
		}
	}
	return nil
}

func (s *Service) scanTorrent(ctx context.Context, torrent qbit.Torrent) error {
	record, exists, err := s.Store.Get(ctx, torrent.Hash)
	if err != nil {
		return err
	}
	if exists && record.AddedOn != 0 && torrent.AddedOn != 0 && record.AddedOn != torrent.AddedOn {
		s.Log.Info("torrent instance changed; refreshing classification", "hash", torrent.Hash, "name", torrent.Name)
		exists = false
	}
	if exists {
		if record.AddedOn == 0 {
			record.AddedOn = torrent.AddedOn
		}
		record.Name, record.Category, record.Tags = torrent.Name, torrent.Category, torrent.Tags
		s.M.TorrentsScanned.Add(1)
		if record.PayloadStatus == string(classifier.Dangerous) {
			s.M.DangerousDetected.Add(1)
			reapplied := !s.dangerousActionSatisfied(torrent)
			if pending(record.ActionTaken) || reapplied {
				action := s.handleDangerous(ctx, torrent, s.target(torrent))
				if reapplied && action != "" {
					action = "re-" + action
				}
				record.ActionTaken = action
			}
			target := s.target(torrent)
			if target != "none" && (pending(record.ReportStatus) || reapplied) {
				record.ReportedTo, record.ReportStatus = target, s.report(ctx, target, torrent.Hash)
			}
		} else if record.PayloadStatus == string(classifier.Suspicious) {
			s.M.SuspiciousDetected.Add(1)
			if pending(record.ActionTaken) {
				record.ActionTaken = s.handleSuspicious(ctx, torrent)
			}
		}
		return s.Store.Save(ctx, record)
	}

	files, err := s.QBit.Files(ctx, torrent.Hash)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		s.M.MetadataUnavailable.Add(1)
		s.Log.Debug("metadata unavailable", "hash", torrent.Hash, "name", torrent.Name)
		return nil
	}
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	result := classifier.Classify(names, s.Config.DangerousExtensions)
	target := s.target(torrent)
	if result.Status == classifier.Suspicious && target == "none" {
		result.Status, result.Reason = classifier.OK, "non-media torrent outside Sonarr/Radarr categories"
	}
	s.M.TorrentsScanned.Add(1)
	if result.Status == classifier.Dangerous {
		s.M.DangerousDetected.Add(1)
	} else if result.Status == classifier.Suspicious {
		s.M.SuspiciousDetected.Add(1)
	}

	record = state.Record{Hash: torrent.Hash, ActionTaken: "", ReportedTo: "none", AddedOn: torrent.AddedOn}
	record.Name, record.Category, record.Tags = torrent.Name, torrent.Category, torrent.Tags
	record.PayloadStatus, record.DangerousFiles = string(result.Status), result.DangerousFiles

	if result.Status == classifier.Dangerous && pending(record.ActionTaken) {
		record.ActionTaken = s.handleDangerous(ctx, torrent, target)
	}
	if result.Status == classifier.Suspicious && pending(record.ActionTaken) {
		record.ActionTaken = s.handleSuspicious(ctx, torrent)
	}
	if result.Status == classifier.Dangerous && target != "none" && pending(record.ReportStatus) {
		record.ReportedTo, record.ReportStatus = target, s.report(ctx, target, torrent.Hash)
	}
	s.Log.Info("payload classified", "hash", torrent.Hash, "name", torrent.Name, "category", torrent.Category,
		"status", result.Status, "reason", result.Reason, "dangerous_files", result.DangerousFiles,
		"action", record.ActionTaken, "reported_to", record.ReportedTo, "report_status", record.ReportStatus)
	return s.Store.Save(ctx, record)
}

func (s *Service) dangerousActionSatisfied(torrent qbit.Torrent) bool {
	if s.Config.DryRun || s.Config.ActionMode == "observe" {
		return true
	}
	if !hasTag(torrent.Tags, "payload-dangerous") {
		return false
	}
	if s.target(torrent) == "none" && !s.Config.PauseUnmapped {
		return true
	}
	switch s.Config.ActionMode {
	case "pause":
		state := strings.ToLower(torrent.State)
		return strings.HasPrefix(state, "stopped") || strings.HasPrefix(state, "paused")
	case "delete":
		// If qBittorrent still returns a torrent that should have been deleted,
		// retry the idempotent delete request.
		return false
	default:
		return true
	}
}

func hasTag(tags, wanted string) bool {
	for _, tag := range strings.Split(tags, ",") {
		if strings.EqualFold(strings.TrimSpace(tag), wanted) {
			return true
		}
	}
	return false
}

func (s *Service) handleDangerous(ctx context.Context, torrent qbit.Torrent, target string) string {
	if s.Config.DryRun {
		return "dry-run"
	}
	if s.Config.ActionMode == "observe" {
		return "observed"
	}
	if err := s.QBit.Tag(ctx, torrent.Hash, "payload-dangerous"); err != nil {
		s.Log.Error("failed to tag dangerous torrent", "hash", torrent.Hash, "error", err)
		return ""
	}
	if target == "none" && !s.Config.PauseUnmapped {
		s.M.QBitActionsTaken.Add(1)
		return "tagged-only"
	}
	switch s.Config.ActionMode {
	case "pause":
		if err := s.QBit.Pause(ctx, torrent.Hash); err != nil {
			s.Log.Error("failed to pause torrent", "hash", torrent.Hash, "error", err)
			return "tagged"
		}
		s.M.QBitActionsTaken.Add(1)
		return "paused"
	case "delete":
		if err := s.QBit.Delete(ctx, torrent.Hash, s.Config.DeleteData); err != nil {
			s.Log.Error("failed to delete torrent", "hash", torrent.Hash, "error", err)
			return "tagged"
		}
		s.M.QBitActionsTaken.Add(1)
		return "deleted"
	default:
		return "tagged"
	}
}

func (s *Service) handleSuspicious(ctx context.Context, torrent qbit.Torrent) string {
	if s.Config.DryRun {
		return "dry-run"
	}
	if s.Config.ActionMode == "observe" {
		return "observed"
	}
	if err := s.QBit.Tag(ctx, torrent.Hash, "payload-suspicious"); err != nil {
		s.Log.Error("failed to tag suspicious torrent", "hash", torrent.Hash, "error", err)
		return ""
	}
	s.M.QBitActionsTaken.Add(1)
	return "tagged"
}

func (s *Service) report(ctx context.Context, target, hash string) string {
	var reporter Reporter
	var okMetric, failedMetric *atomic.Uint64
	if target == "sonarr" {
		reporter, okMetric, failedMetric = s.Sonarr, &s.M.SonarrReportsOK, &s.M.SonarrReportsFailed
	} else {
		reporter, okMetric, failedMetric = s.Radarr, &s.M.RadarrReportsOK, &s.M.RadarrReportsFailed
	}
	if s.Config.DryRun || s.Config.ActionMode == "observe" {
		return "dry-run"
	}
	found, err := reporter.ReportFailed(ctx, hash)
	if err != nil {
		failedMetric.Add(1)
		s.Log.Error("Arr report failed", "target", target, "hash", hash, "error", err)
		return "failed"
	}
	if !found {
		failedMetric.Add(1)
		return "queue-item-not-found"
	}
	okMetric.Add(1)
	return "blocklisted"
}

func pending(value string) bool {
	return value == "" || value == "dry-run" || value == "observed" ||
		value == "failed" || value == "queue-item-not-found"
}

func (s *Service) target(torrent qbit.Torrent) string {
	values := []string{strings.ToLower(torrent.Category)}
	for _, tag := range strings.Split(strings.ToLower(torrent.Tags), ",") {
		values = append(values, strings.TrimSpace(tag))
	}
	for _, value := range values {
		if slices.Contains(s.Config.SonarrCategories, value) {
			return "sonarr"
		}
		if slices.Contains(s.Config.RadarrCategories, value) {
			return "radarr"
		}
	}
	return "none"
}
