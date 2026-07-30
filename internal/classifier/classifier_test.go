package classifier

import "testing"

var danger = []string{".exe", ".scr"}

func TestClassify(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  Status
	}{
		{"normal episode", []string{"Show.S01E01.mkv"}, OK},
		{"only scr", []string{"Show.S01E01.scr"}, Dangerous},
		{"video and executable", []string{"movie.mkv", "codec.EXE"}, Dangerous},
		{"no video", []string{"readme.txt"}, Suspicious},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.files, danger).Status; got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}
