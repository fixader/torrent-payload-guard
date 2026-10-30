package archive

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		text       string
		unresolved bool
		count      int
	}{
		{"Path = movie.mkv\nEncrypted = -\n", false, 1},
		{"Path = movie.mkv\n\nPath = setup.exe\n", false, 2},
		{"Path = movie.mkv\nEncrypted = +\n", true, 1},
		{"Path = inner.7z\n", true, 1},
	} {
		names, pending, err := Parse(tc.text)
		if err != nil || pending != tc.unresolved || len(names) != tc.count {
			t.Fatalf("parse: %v %v %v", names, pending, err)
		}
	}
	if _, _, err := Parse("broken"); err == nil {
		t.Fatal("accepted invalid listing")
	}
	if _, _, err := Parse(strings.Repeat("Path = x\n", 10001)); err == nil {
		t.Fatal("unbounded listing")
	}
}

func TestResolveRejectsEscapeAndSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "test.zip"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root, "/downloads", "/downloads/test.zip"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/etc/passwd", "/downloads/../test.zip", "/downloads"} {
		if _, err := Resolve(root, "/downloads", path); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	if err := os.Symlink(filepath.Join(root, "test.zip"), filepath.Join(root, "link.zip")); err == nil {
		if _, err := Resolve(root, "/downloads", "/downloads/link.zip"); err == nil {
			t.Fatal("accepted symlink")
		}
	}
}

// Harmless fixtures exercise the real listing command; nothing is executable.
func TestRealArchives(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux resource limits")
	}
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("7z unavailable")
	}
	for _, ext := range []string{"7z", "zip"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "episode.exe"), []byte("harmless test fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "payload."+ext)
		cmd := exec.Command("7z", "a", path, "episode.exe")
		cmd.Dir = dir
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture: %s %v", b, err)
		}
		names, _, err := List(context.Background(), path)
		if err != nil || len(names) != 1 || names[0] != "episode.exe" {
			t.Fatalf("listing %s: %v %v", ext, names, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, _, err := List(ctx, path); err == nil {
			t.Fatal("ignored cancellation")
		}
	}
}
