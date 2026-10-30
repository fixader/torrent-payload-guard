package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"torrentguard/internal/archive"
	"torrentguard/internal/classifier"
	"torrentguard/internal/qbit"
)

func (s *Service) classify(ctx context.Context, torrent qbit.Torrent, files []qbit.File) classifier.Result {
	result := classifier.Classify(selectedFileNames(files), s.Config.DangerousExtensions)
	if result.Status == classifier.Dangerous || s.target(torrent) == "none" {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	count := 0
	for _, file := range files {
		if file.Priority == 0 || !archive.IsArchive(file.Name) {
			continue
		}
		count++
		// Do not read a sparse/unfinished archive as if it were a valid payload.
		if file.Progress < 1 || count > 4 || ctx.Err() != nil {
			result.ArchiveUnresolved = true
			continue
		}
		name := filepath.Clean(file.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) || strings.Contains(file.Name, "\\") {
			result.ArchiveUnresolved = true
			continue
		}
		path := filepath.Join(torrent.SavePath, name)
		// content_path follows qBit's incomplete -> completed move, unlike save_path.
		if torrent.ContentPath != "" {
			if len(files) == 1 {
				path = torrent.ContentPath
			} else if strings.Split(name, string(filepath.Separator))[0] == filepath.Base(torrent.ContentPath) {
				path = filepath.Join(filepath.Dir(torrent.ContentPath), name)
			}
		}
		local, err := archive.Resolve(s.Config.ArchiveRoot, s.Config.QBitDownloadRoot, path)
		if err != nil {
			result.ArchiveUnresolved = true
			continue
		}
		st, err := os.Stat(local)
		if err != nil {
			result.ArchiveUnresolved = true
			continue
		}
		fingerprint := fmt.Sprintf("v1:%s:%d:%d:%v", local, st.Size(), st.ModTime().UnixNano(), s.Config.DangerousExtensions)
		var inner classifier.Result
		cached := false
		if s.Store != nil {
			data, found, e := s.Store.ArchiveResult(ctx, torrent.Hash, file.Name, fingerprint)
			cached = e == nil && found && json.Unmarshal([]byte(data), &inner) == nil
		}
		if !cached {
			names, unresolved, e := s.archiveList(ctx, local)
			if e != nil {
				s.Log.Warn("archive inspection pending", "hash", torrent.Hash, "file", file.Name, "error", e)
				result.ArchiveUnresolved = true
				continue
			}
			inner = classifier.Classify(names, s.Config.DangerousExtensions)
			inner.ArchiveUnresolved = unresolved
			if s.Store != nil {
				data, _ := json.Marshal(inner)
				if e := s.Store.SaveArchiveResult(ctx, torrent.Hash, file.Name, fingerprint, string(data)); e != nil {
					s.Log.Warn("archive result cache unavailable", "hash", torrent.Hash, "error", e)
				}
			}
		}

		for _, bad := range inner.DangerousFiles {
			result.DangerousFiles = append(result.DangerousFiles, file.Name+" ! "+bad)
		}
		for _, video := range inner.VideoFiles {
			result.VideoFiles = append(result.VideoFiles, file.Name+" ! "+video)
		}
		result.ArchiveUnresolved = result.ArchiveUnresolved || inner.ArchiveUnresolved
	}
	if len(result.DangerousFiles) > 0 {
		result.Status, result.Reason = classifier.Dangerous, "dangerous file inside archive"
	} else if result.ArchiveUnresolved {
		result.Status, result.Reason = classifier.Suspicious, "archive inspection pending or encrypted/nested content"
	} else if len(result.VideoFiles) > 0 {
		result.Status, result.Reason = classifier.OK, "expected video payload found"
	}
	return result
}
