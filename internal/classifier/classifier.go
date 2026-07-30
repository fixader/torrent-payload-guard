package classifier

import (
	"path/filepath"
	"strings"
)

type Status string

const (
	OK         Status = "ok"
	Suspicious Status = "suspicious"
	Dangerous  Status = "dangerous"
)

var videoExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true,
	".m4v": true, ".ts": true, ".m2ts": true,
}

type Result struct {
	Status         Status   `json:"status"`
	DangerousFiles []string `json:"dangerous_files"`
	VideoFiles     []string `json:"video_files"`
	Reason         string   `json:"reason"`
}

func Classify(files []string, dangerousExtensions []string) Result {
	dangerous := make(map[string]bool, len(dangerousExtensions))
	for _, extension := range dangerousExtensions {
		dangerous[strings.ToLower(extension)] = true
	}
	result := Result{DangerousFiles: []string{}, VideoFiles: []string{}}
	for _, file := range files {
		extension := strings.ToLower(filepath.Ext(file))
		if dangerous[extension] {
			result.DangerousFiles = append(result.DangerousFiles, file)
		}
		if videoExtensions[extension] {
			result.VideoFiles = append(result.VideoFiles, file)
		}
	}
	if len(result.DangerousFiles) > 0 {
		result.Status, result.Reason = Dangerous, "one or more files have a dangerous extension"
	} else if len(result.VideoFiles) == 0 {
		result.Status, result.Reason = Suspicious, "no expected video files found"
	} else {
		result.Status, result.Reason = OK, "expected video payload found"
	}
	return result
}
