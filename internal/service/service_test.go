package service

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"torrentguard/internal/config"
	"torrentguard/internal/qbit"
	"torrentguard/internal/state"
)

type fakeQBit struct {
	torrents  []qbit.Torrent
	files     map[string][]qbit.File
	fileCalls int
	tags      int
	pauses    int
	deletes   int
}

func (f *fakeQBit) Login(context.Context) error                      { return nil }
func (f *fakeQBit) Torrents(context.Context) ([]qbit.Torrent, error) { return f.torrents, nil }
func (f *fakeQBit) Files(_ context.Context, hash string) ([]qbit.File, error) {
	f.fileCalls++
	return f.files[hash], nil
}
func (f *fakeQBit) Tag(_ context.Context, hash, tag string) error {
	f.tags++
	for i := range f.torrents {
		if f.torrents[i].Hash == hash {
			if f.torrents[i].Tags != "" {
				f.torrents[i].Tags += ", "
			}
			f.torrents[i].Tags += tag
		}
	}
	return nil
}
func (f *fakeQBit) Pause(_ context.Context, hash string) error {
	f.pauses++
	for i := range f.torrents {
		if f.torrents[i].Hash == hash {
			f.torrents[i].State = "stoppedDL"
		}
	}
	return nil
}
func (f *fakeQBit) Delete(context.Context, string, bool) error { f.deletes++; return nil }

type fakeReporter struct{ calls int }

func (f *fakeReporter) ReportFailed(context.Context, string) (bool, error) {
	f.calls++
	return true, nil
}

func TestRepeatedPollDoesNotDuplicateActionOrReport(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	qb := &fakeQBit{
		torrents: []qbit.Torrent{{Hash: "abc", Name: "bad", Category: "sonarr"}},
		files:    map[string][]qbit.File{"abc": {{Name: "episode.scr"}}},
	}
	reporter := &fakeReporter{}
	cfg := config.Config{
		DangerousExtensions: []string{".scr"}, ActionMode: "pause", DryRun: false,
		SonarrCategories: []string{"sonarr"}, RadarrCategories: []string{"radarr"},
	}
	svc := New(cfg, qb, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Sonarr = reporter
	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if qb.tags != 1 || qb.pauses != 1 || reporter.calls != 1 {
		t.Fatalf("duplicate side effect: tags=%d pauses=%d reports=%d", qb.tags, qb.pauses, reporter.calls)
	}
}

func TestReaddedDangerousHashIsProtectedAgain(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	qb := &fakeQBit{
		torrents: []qbit.Torrent{{Hash: "same-hash", Name: "bad", Category: "radarr", State: "downloading"}},
		files:    map[string][]qbit.File{"same-hash": {{Name: "movie.exe"}}},
	}
	reporter := &fakeReporter{}
	cfg := config.Config{
		DangerousExtensions: []string{".exe"}, ActionMode: "pause", DryRun: false,
		SonarrCategories: []string{"sonarr"}, RadarrCategories: []string{"radarr"},
	}
	svc := New(cfg, qb, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Radarr = reporter
	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Simulate qBittorrent removing and later re-adding the exact same infohash.
	qb.torrents[0].Tags = ""
	qb.torrents[0].State = "downloading"
	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if qb.tags != 2 || qb.pauses != 2 || reporter.calls != 2 {
		t.Fatalf("re-added torrent was not protected again: tags=%d pauses=%d reports=%d", qb.tags, qb.pauses, reporter.calls)
	}
	record, exists, err := store.Get(context.Background(), "same-hash")
	if err != nil || !exists {
		t.Fatalf("missing state record: exists=%v err=%v", exists, err)
	}
	if record.ActionTaken != "re-paused" {
		t.Fatalf("dashboard action = %q, want re-paused", record.ActionTaken)
	}

	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if qb.tags != 2 || qb.pauses != 2 || reporter.calls != 2 {
		t.Fatalf("stable stopped torrent caused duplicate actions: tags=%d pauses=%d reports=%d", qb.tags, qb.pauses, reporter.calls)
	}
}

func TestMetadataUnavailableDoesNothing(t *testing.T) {
	store, _ := state.Open(filepath.Join(t.TempDir(), "test.db"))
	defer store.Close()
	qb := &fakeQBit{torrents: []qbit.Torrent{{Hash: "abc", Category: "sonarr"}}, files: map[string][]qbit.File{}}
	cfg := config.Config{DangerousExtensions: []string{".scr"}, SonarrCategories: []string{"sonarr"}}
	svc := New(cfg, qb, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if svc.M.MetadataUnavailable.Load() != 1 || qb.tags != 0 {
		t.Fatal("metadata-unavailable torrent was not safely skipped")
	}
}

func TestOKTorrentIsCachedUntilInstanceChanges(t *testing.T) {
	store, _ := state.Open(filepath.Join(t.TempDir(), "test.db"))
	defer store.Close()
	qb := &fakeQBit{
		torrents: []qbit.Torrent{{Hash: "movie", Name: "movie", AddedOn: 100}},
		files:    map[string][]qbit.File{"movie": {{Name: "movie.mkv"}}},
	}
	cfg := config.Config{DangerousExtensions: []string{".exe"}}
	svc := New(cfg, qb, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if qb.fileCalls != 1 {
		t.Fatalf("unchanged OK torrent file list fetched %d times, want 1", qb.fileCalls)
	}

	qb.torrents[0].AddedOn = 200
	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if qb.fileCalls != 2 {
		t.Fatalf("new torrent instance file list fetched %d times, want 2", qb.fileCalls)
	}
}

func TestDeletedTorrentIsRemovedFromDashboardState(t *testing.T) {
	store, _ := state.Open(filepath.Join(t.TempDir(), "test.db"))
	defer store.Close()
	qb := &fakeQBit{
		torrents: []qbit.Torrent{{Hash: "gone", Name: "movie"}},
		files:    map[string][]qbit.File{"gone": {{Name: "movie.mkv"}}},
	}
	svc := New(config.Config{}, qb, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	qb.torrents = nil
	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := store.Get(context.Background(), "gone"); err != nil || exists {
		t.Fatalf("deleted torrent retained in state: exists=%v err=%v", exists, err)
	}
}

func TestUnmappedDangerousTorrentCanBeTagOnly(t *testing.T) {
	store, _ := state.Open(filepath.Join(t.TempDir(), "test.db"))
	defer store.Close()
	qb := &fakeQBit{
		torrents: []qbit.Torrent{{Hash: "unmapped", Name: "bad"}},
		files:    map[string][]qbit.File{"unmapped": {{Name: "payload.exe"}}},
	}
	cfg := config.Config{
		DangerousExtensions: []string{".exe"}, ActionMode: "pause",
		PauseUnmapped: false,
	}
	svc := New(cfg, qb, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if qb.tags != 1 || qb.pauses != 0 || qb.deletes != 0 {
		t.Fatalf("expected tag-only behavior: tags=%d pauses=%d deletes=%d", qb.tags, qb.pauses, qb.deletes)
	}
}
