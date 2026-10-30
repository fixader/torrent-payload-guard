package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"torrentguard/internal/config"
	"torrentguard/internal/qbit"
	"torrentguard/internal/state"
)

func TestArchiveLifecycle(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "episode.7z"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	qb := &fakeQBit{torrents: []qbit.Torrent{{Hash: "archive", Category: "sonarr", SavePath: "/downloads", ContentPath: "/downloads/episode.7z"}}, files: map[string][]qbit.File{"archive": {{Name: "episode.7z", Priority: 1}}}}
	cfg := config.Config{ArchiveRoot: root, QBitDownloadRoot: "/downloads", DangerousExtensions: []string{".exe"}, ActionMode: "pause", SonarrCategories: []string{"sonarr"}}
	svc := New(cfg, qb, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	report := &fakeReporter{}
	svc.Sonarr = report
	calls := 0
	unreadable := false
	svc.archiveList = func(context.Context, string) ([]string, bool, error) {
		calls++
		if unreadable {
			return nil, true, errors.New("unreadable")
		}
		return []string{"episode.mkv", "codec.exe"}, false, nil
	}
	scan := func() {
		t.Helper()
		if err := svc.Scan(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	scan()
	if qb.pauses != 0 || calls != 0 {
		t.Fatal("read incomplete archive")
	}
	qb.torrents[0].Progress = 1
	qb.files["archive"][0].Progress = 1
	scan()
	if qb.pauses != 1 || report.calls != 1 {
		t.Fatalf("did not stop/report archive: %d %d", qb.pauses, report.calls)
	}
	scan()
	if qb.pauses != 1 || report.calls != 1 || qb.resumes != 0 {
		t.Fatal("repeated side effects")
	}
	unreadable = true
	svc.fileChecks["archive"] = time.Now().Add(-6 * time.Minute)
	os.Remove(filepath.Join(root, "episode.7z"))
	scan()
	if qb.resumes != 0 {
		t.Fatal("read failure released dangerous archive")
	}
	qb.files["archive"][0].Priority = 0
	svc.fileChecks["archive"] = time.Now().Add(-6 * time.Minute)
	qb.files["archive"] = append(qb.files["archive"], qbit.File{Name: "real.mkv", Priority: 1, Progress: 1})
	scan()
	if qb.resumes != 1 {
		t.Fatal("deselection did not release remaining safe video")
	}
}

func TestCachedOKPayloadRecheckedAfterCompletion(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "extra.zip"), []byte("fixture"), 0600)
	store, err := state.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	qb := &fakeQBit{torrents: []qbit.Torrent{{Hash: "old", Category: "sonarr", Progress: 1, SavePath: "/downloads"}}, files: map[string][]qbit.File{"old": {{Name: "video.mkv", Priority: 1, Progress: 1}, {Name: "extra.zip", Priority: 1, Progress: 1}}}}
	if err := store.Save(context.Background(), state.Record{Hash: "old", PayloadStatus: "ok"}); err != nil {
		t.Fatal(err)
	}
	svc := New(config.Config{ArchiveRoot: root, QBitDownloadRoot: "/downloads", DangerousExtensions: []string{".exe"}, ActionMode: "pause", SonarrCategories: []string{"sonarr"}}, qb, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Sonarr = &fakeReporter{}
	svc.archiveList = func(context.Context, string) ([]string, bool, error) { return []string{"payload.exe"}, false, nil }
	svc.Scan(context.Background())
	if qb.pauses != 1 {
		t.Fatal("old OK verdict bypassed archive inspection")
	}
}

func TestUnknownArchiveDoesNotBecomeOKBesideVideo(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "extra.zip"), []byte("fixture"), 0600)
	svc := &Service{Config: config.Config{ArchiveRoot: root, QBitDownloadRoot: "/downloads", SonarrCategories: []string{"sonarr"}}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), archiveList: func(context.Context, string) ([]string, bool, error) { return []string{"inside.mkv"}, true, nil }}
	r := svc.classify(context.Background(), qbit.Torrent{Category: "sonarr", SavePath: "/downloads"}, []qbit.File{{Name: "real.mkv", Priority: 1}, {Name: "extra.zip", Priority: 1, Progress: 1}})
	if r.Status != "suspicious" || !r.ArchiveUnresolved {
		t.Fatalf("encrypted/nested archive cleared: %+v", r)
	}
}

func TestRealArchiveStopsAndReports(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux archive listing")
	}
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("7z not installed")
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "isooy1.exe"), []byte("harmless fixture, never executed"), 0600)
	cmd := exec.Command("7z", "a", filepath.Join(root, "episode.7z"), "isooy1.exe")
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture %s %v", b, err)
	}
	store, err := state.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	qb := &fakeQBit{torrents: []qbit.Torrent{{Hash: "real", Category: "sonarr", SavePath: "/downloads", ContentPath: "/downloads/episode.7z", Progress: 1}}, files: map[string][]qbit.File{"real": {{Name: "episode.7z", Priority: 1, Progress: 1}}}}
	svc := New(config.Config{ArchiveRoot: root, QBitDownloadRoot: "/downloads", DangerousExtensions: []string{".exe"}, ActionMode: "pause", SonarrCategories: []string{"sonarr"}}, qb, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	report := &fakeReporter{}
	svc.Sonarr = report
	if err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, ok, err := store.Get(context.Background(), "real")
	if err != nil || !ok || record.PayloadStatus != "dangerous" || qb.pauses != 1 || report.calls != 1 {
		t.Fatalf("real archive bypassed guard: %+v pauses=%d reports=%d err=%v", record, qb.pauses, report.calls, err)
	}
}

func TestArchiveResultPersistsAndInvalidates(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "episode.7z")
	os.WriteFile(file, []byte("fixture"), 0600)
	store, err := state.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	qb := &fakeQBit{torrents: []qbit.Torrent{{Hash: "cache", Category: "sonarr", SavePath: "/downloads", ContentPath: "/downloads/episode.7z", Progress: 1}}, files: map[string][]qbit.File{"cache": {{Name: "episode.7z", Priority: 1, Progress: 1}}}}
	cfg := config.Config{ArchiveRoot: root, QBitDownloadRoot: "/downloads", DangerousExtensions: []string{".exe"}, ActionMode: "pause", SonarrCategories: []string{"sonarr"}}
	calls := 0
	build := func() *Service {
		svc := New(cfg, qb, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
		svc.Sonarr = &fakeReporter{}
		svc.archiveList = func(context.Context, string) ([]string, bool, error) {
			calls++
			return []string{"movie.mkv"}, false, nil
		}
		return svc
	}
	svc := build()
	svc.Scan(context.Background())
	for i := 0; i < 100; i++ {
		svc.Scan(context.Background())
	}
	if calls != 1 || qb.fileCalls != 1 {
		t.Fatalf("unchanged archive rescanned: parser=%d files=%d", calls, qb.fileCalls)
	}
	svc = build()
	svc.Scan(context.Background())
	if calls != 1 {
		t.Fatal("restart lost durable archive result")
	}
	os.WriteFile(file, []byte("changed-size"), 0600)
	svc = build()
	svc.Scan(context.Background())
	if calls != 2 {
		t.Fatal("changed archive reused old result")
	}
}

func TestBlockedFileSelectionPollingIsBounded(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	qb := &fakeQBit{torrents: []qbit.Torrent{{Hash: "blocked", Category: "sonarr"}}, files: map[string][]qbit.File{"blocked": {{Name: "setup.exe", Priority: 1}}}}
	svc := New(config.Config{DangerousExtensions: []string{".exe"}, ActionMode: "pause", SonarrCategories: []string{"sonarr"}}, qb, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Sonarr = &fakeReporter{}
	for i := 0; i < 100; i++ {
		svc.Scan(context.Background())
	}
	if qb.fileCalls != 1 || qb.pauses != 1 {
		t.Fatalf("repeated unchanged polling: files=%d pauses=%d", qb.fileCalls, qb.pauses)
	}
	svc.fileChecks["blocked"] = time.Now().Add(-6 * time.Minute)
	svc.Scan(context.Background())
	if qb.fileCalls != 2 {
		t.Fatal("selection fallback not run")
	}
}
