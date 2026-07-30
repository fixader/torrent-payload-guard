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
	torrents []qbit.Torrent
	files    map[string][]qbit.File
	tags     int
	pauses   int
	deletes  int
}

func (f *fakeQBit) Login(context.Context) error                      { return nil }
func (f *fakeQBit) Torrents(context.Context) ([]qbit.Torrent, error) { return f.torrents, nil }
func (f *fakeQBit) Files(_ context.Context, hash string) ([]qbit.File, error) {
	return f.files[hash], nil
}
func (f *fakeQBit) Tag(context.Context, string, string) error  { f.tags++; return nil }
func (f *fakeQBit) Pause(context.Context, string) error        { f.pauses++; return nil }
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
