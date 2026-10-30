// Package archive lists archive members without extracting or executing them.
package archive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func IsArchive(name string) bool {
	n := strings.ToLower(name)
	for _, suffix := range []string{".7z", ".zip", ".rar", ".tar", ".gz", ".bz2", ".xz", ".iso", ".7z.001", ".zip.001"} {
		if strings.HasSuffix(n, suffix) {
			return true
		}
	}
	return false
}

// Resolve maps a qBit path into a read-only mount. Neither traversal nor
// symlinks may redirect inspection outside that mount.
func Resolve(root, qbitRoot, path string) (string, error) {
	if root == "" || qbitRoot == "" {
		return "", errors.New("archive access not configured")
	}
	rel, err := filepath.Rel(filepath.Clean(qbitRoot), filepath.Clean(path))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("archive path outside download root")
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(base, rel)
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	if real != full {
		return "", errors.New("archive symlink refused")
	}
	st, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("archive is not a regular file")
	}
	return full, nil
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4*1024*1024 {
		return 0, errors.New("archive listing too large")
	}
	return b.Buffer.Write(p)
}

// List uses a fixed shell program solely to apply per-process resource limits.
// The filename is a separate argument, never shell source. No archive writes.
func List(ctx context.Context, path string) ([]string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `ulimit -v 262144 || exit 1; ulimit -t 2 || exit 1; exec 7z l -slt -ba -sccUTF-8 -p -mmt=1 -- "$1"`, "archive-list", path)
	cmd.WaitDelay = 250 * time.Millisecond
	var out boundedOutput
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, true, fmt.Errorf("archive listing unavailable: %w", err)
	}
	return Parse(out.String())
}

// -ba omits the outer archive header, so Path records refer only to members.
// Encrypted/nested payloads remain unresolved even when filenames are visible.
func Parse(text string) ([]string, bool, error) {
	var names []string
	unresolved := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "Path = ") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "Path = "))
			if name == "" {
				return nil, true, errors.New("empty archive member")
			}
			names = append(names, name)
			if len(names) > 10000 {
				return nil, true, errors.New("too many archive members")
			}
			if IsArchive(name) {
				unresolved = true
			}
		}
		if line == "Encrypted = +" {
			unresolved = true
		}
	}
	if len(names) == 0 {
		return nil, true, errors.New("no archive members listed")
	}
	return names, unresolved, nil
}
