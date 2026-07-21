package diff

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type LineKind uint8

const (
	Context LineKind = iota
	Addition
	Deletion
	Meta
)

type Line struct {
	Kind      LineKind `json:"kind"`
	Text      string   `json:"text"`
	OldNumber int      `json:"old_number,omitempty"`
	NewNumber int      `json:"new_number,omitempty"`
	Hunk      int      `json:"hunk"`
	// OriginalIndex is assigned by the UI display cache after filtering. It is
	// intentionally excluded from persisted snapshots and parser output.
	OriginalIndex int `json:"-"`
	// Collapsed is set only on synthetic UI rows that represent omitted,
	// unchanged source between adjacent Git hunks.
	Collapsed int `json:"-"`
}

type Hunk struct {
	Header string `json:"header"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
}

type File struct {
	OldPath   string `json:"old_path"`
	Path      string `json:"path"`
	Status    string `json:"status"`
	Binary    bool   `json:"binary"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Lines     []Line `json:"lines"`
	Hunks     []Hunk `json:"hunks"`
}

var hunkRE = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func Parse(input string) ([]File, error) {
	var files []File
	var current *File
	oldLine, newLine, hunkIndex := 0, 0, -1

	flush := func() {
		if current == nil {
			return
		}
		if current.Path == "" {
			current.Path = current.OldPath
		}
		if current.Status == "" {
			current.Status = "M"
		}
		files = append(files, *current)
		current = nil
	}

	scanner := bufio.NewScanner(strings.NewReader(input))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		raw := scanner.Text()
		if strings.HasPrefix(raw, "diff --git ") {
			flush()
			oldPath, newPath := parseDiffHeader(raw)
			current = &File{OldPath: oldPath, Path: newPath, Status: "M"}
			hunkIndex = -1
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case strings.HasPrefix(raw, "new file mode"):
			current.Status = "A"
		case strings.HasPrefix(raw, "deleted file mode"):
			current.Status = "D"
		case strings.HasPrefix(raw, "rename from "):
			current.Status = "R"
			current.OldPath = strings.TrimPrefix(raw, "rename from ")
		case strings.HasPrefix(raw, "rename to "):
			current.Status = "R"
			current.Path = strings.TrimPrefix(raw, "rename to ")
		case strings.HasPrefix(raw, "Binary files ") || strings.HasPrefix(raw, "GIT binary patch"):
			current.Binary = true
		case strings.HasPrefix(raw, "@@"):
			matches := hunkRE.FindStringSubmatch(raw)
			if len(matches) != 3 {
				return nil, fmt.Errorf("invalid hunk header: %s", raw)
			}
			oldLine, _ = strconv.Atoi(matches[1])
			newLine, _ = strconv.Atoi(matches[2])
			hunkIndex++
			current.Hunks = append(current.Hunks, Hunk{Header: raw, Start: len(current.Lines)})
			current.Lines = append(current.Lines, Line{Kind: Meta, Text: raw, Hunk: hunkIndex})
		case hunkIndex >= 0 && strings.HasPrefix(raw, "+") && !strings.HasPrefix(raw, "+++"):
			current.Lines = append(current.Lines, Line{Kind: Addition, Text: strings.TrimPrefix(raw, "+"), NewNumber: newLine, Hunk: hunkIndex})
			current.Additions++
			newLine++
		case hunkIndex >= 0 && strings.HasPrefix(raw, "-") && !strings.HasPrefix(raw, "---"):
			current.Lines = append(current.Lines, Line{Kind: Deletion, Text: strings.TrimPrefix(raw, "-"), OldNumber: oldLine, Hunk: hunkIndex})
			current.Deletions++
			oldLine++
		case hunkIndex >= 0 && strings.HasPrefix(raw, " "):
			current.Lines = append(current.Lines, Line{Kind: Context, Text: strings.TrimPrefix(raw, " "), OldNumber: oldLine, NewNumber: newLine, Hunk: hunkIndex})
			oldLine++
			newLine++
		case hunkIndex >= 0 && strings.HasPrefix(raw, `\ No newline`):
			current.Lines = append(current.Lines, Line{Kind: Meta, Text: raw, Hunk: hunkIndex})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if current != nil && len(current.Hunks) > 0 {
		current.Hunks[len(current.Hunks)-1].End = len(current.Lines)
	}
	flush()
	for fileIndex := range files {
		for hunk := range files[fileIndex].Hunks {
			if files[fileIndex].Hunks[hunk].End == 0 {
				if hunk+1 < len(files[fileIndex].Hunks) {
					files[fileIndex].Hunks[hunk].End = files[fileIndex].Hunks[hunk+1].Start
				} else {
					files[fileIndex].Hunks[hunk].End = len(files[fileIndex].Lines)
				}
			}
		}
	}
	return files, nil
}

func parseDiffHeader(raw string) (string, string) {
	parts := strings.SplitN(strings.TrimPrefix(raw, "diff --git "), " b/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimPrefix(parts[0], "a/"), parts[1]
}

func (k LineKind) Marker() string {
	switch k {
	case Addition:
		return "+"
	case Deletion:
		return "-"
	case Meta:
		return "@"
	default:
		return " "
	}
}
