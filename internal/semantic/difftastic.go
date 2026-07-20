package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxDifftasticOutputBytes = 32 << 20

type difftasticRunner interface {
	Run(context.Context, string, string) ([]byte, error)
}

type execDifftasticRunner struct{}

func (execDifftasticRunner) Run(ctx context.Context, oldPath, newPath string) ([]byte, error) {
	command := exec.CommandContext(ctx, "difft", "--display", "json", "--color", "never", "--strip-cr", "off", oldPath, newPath)
	command.Env = environmentWith(os.Environ(), "DFT_UNSTABLE", "yes")
	stdout, stderr := &limitedBuffer{limit: maxDifftasticOutputBytes}, &limitedBuffer{limit: 1 << 20}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	return stdout.Bytes(), nil
}

func environmentWith(environment []string, key, value string) []string {
	prefix := key + "="
	updated := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			updated = append(updated, entry)
		}
	}
	return append(updated, prefix+value)
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Write(content []byte) (int, error) {
	if b.buffer.Len()+len(content) > b.limit {
		return 0, fmt.Errorf("difftastic output exceeds %d bytes", b.limit)
	}
	return b.buffer.Write(content)
}

func (b *limitedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *limitedBuffer) String() string { return b.buffer.String() }

type difftasticAdapter struct {
	runner difftasticRunner
}

func (d difftasticAdapter) supports(string) bool { return true }

func (d difftasticAdapter) analyze(ctx context.Context, input Input) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	if len(input.Old) > maxSemanticSourceBytes || len(input.New) > maxSemanticSourceBytes {
		return Plan{}, fmt.Errorf("source exceeds the %d-byte difftastic budget", maxSemanticSourceBytes)
	}
	temporary, err := os.MkdirTemp("", "revui-difftastic-*")
	if err != nil {
		return Plan{}, fmt.Errorf("create difftastic workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporary) }()
	oldDirectory, newDirectory := filepath.Join(temporary, "old"), filepath.Join(temporary, "new")
	if err := os.Mkdir(oldDirectory, 0o700); err != nil {
		return Plan{}, fmt.Errorf("create difftastic old directory: %w", err)
	}
	if err := os.Mkdir(newDirectory, 0o700); err != nil {
		return Plan{}, fmt.Errorf("create difftastic new directory: %w", err)
	}
	name := filepath.Base(filepath.FromSlash(input.Path))
	if name == "." || name == ".." || name == string(filepath.Separator) || name == "" {
		name = "source.txt"
	}
	oldPath, newPath := filepath.Join(oldDirectory, name), filepath.Join(newDirectory, name)
	if err := os.WriteFile(oldPath, input.Old, 0o600); err != nil {
		return Plan{}, fmt.Errorf("write difftastic old source: %w", err)
	}
	if err := os.WriteFile(newPath, input.New, 0o600); err != nil {
		return Plan{}, fmt.Errorf("write difftastic new source: %w", err)
	}
	runner := d.runner
	if runner == nil {
		runner = execDifftasticRunner{}
	}
	output, err := runner.Run(ctx, oldPath, newPath)
	if err != nil {
		return Plan{}, fmt.Errorf("run difftastic: %w", err)
	}
	return parseDifftasticPlan(input, output)
}

type difftasticOutput struct {
	AlignedLines [][]*int            `json:"aligned_lines"`
	Chunks       [][]difftasticChunk `json:"chunks"`
	Language     string              `json:"language"`
	Status       string              `json:"status"`
}

type difftasticChunk struct {
	LHS *difftasticSide `json:"lhs"`
	RHS *difftasticSide `json:"rhs"`
}

type difftasticSide struct {
	LineNumber int                `json:"line_number"`
	Changes    []difftasticChange `json:"changes"`
}

type difftasticChange struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Content string `json:"content"`
}

func parseDifftasticPlan(input Input, data []byte) (Plan, error) {
	outputs, err := decodeDifftasticOutputs(data)
	if err != nil {
		return Plan{}, err
	}
	if len(outputs) != 1 {
		return Plan{}, fmt.Errorf("difftastic returned %d file results, want 1", len(outputs))
	}
	output := outputs[0]
	plan := Plan{Engine: EngineDifftastic}
	oldLines, newLines := sourceLineRanges(input.Old), sourceLineRanges(input.New)
	for _, pair := range output.AlignedLines {
		if len(pair) != 2 {
			return Plan{}, errors.New("difftastic returned an invalid aligned line pair")
		}
		alignment := LineAlignment{}
		if pair[0] != nil && *pair[0] >= 0 && *pair[0] < len(oldLines) {
			alignment.Old = *pair[0] + 1
		}
		if pair[1] != nil && *pair[1] >= 0 && *pair[1] < len(newLines) {
			alignment.New = *pair[1] + 1
		}
		if alignment.Old != 0 || alignment.New != 0 {
			plan.Alignment = append(plan.Alignment, alignment)
		}
	}
	for _, chunk := range output.Chunks {
		for _, row := range chunk {
			if row.LHS != nil {
				ranges, rangeErr := difftasticSideRanges(input.Old, oldLines, *row.LHS)
				if rangeErr != nil {
					return Plan{}, rangeErr
				}
				plan.Old = append(plan.Old, ranges...)
			}
			if row.RHS != nil {
				ranges, rangeErr := difftasticSideRanges(input.New, newLines, *row.RHS)
				if rangeErr != nil {
					return Plan{}, rangeErr
				}
				plan.New = append(plan.New, ranges...)
			}
		}
	}
	sortRanges(plan.Old)
	sortRanges(plan.New)
	return plan, nil
}

func decodeDifftasticOutputs(data []byte) ([]difftasticOutput, error) {
	var many []difftasticOutput
	if err := json.Unmarshal(data, &many); err == nil {
		return many, nil
	}
	var one difftasticOutput
	if err := json.Unmarshal(data, &one); err != nil {
		return nil, fmt.Errorf("decode difftastic JSON: %w", err)
	}
	return []difftasticOutput{one}, nil
}

type sourceLineRange struct {
	start, end int
}

func sourceLineRanges(source []byte) []sourceLineRange {
	if len(source) == 0 {
		return nil
	}
	var lines []sourceLineRange
	start := 0
	for index, value := range source {
		if value != '\n' {
			continue
		}
		end := index
		if end > start && source[end-1] == '\r' {
			end--
		}
		lines = append(lines, sourceLineRange{start: start, end: end})
		start = index + 1
	}
	if start < len(source) {
		lines = append(lines, sourceLineRange{start: start, end: len(source)})
	}
	return lines
}

func difftasticSideRanges(source []byte, lines []sourceLineRange, side difftasticSide) ([]Range, error) {
	if side.LineNumber < 0 || side.LineNumber >= len(lines) {
		return nil, errors.New("difftastic returned an out-of-range line number")
	}
	line := lines[side.LineNumber]
	lineLength := line.end - line.start
	ranges := make([]Range, 0, len(side.Changes))
	for _, change := range side.Changes {
		if change.Start < 0 || change.End < change.Start || change.End > lineLength {
			return nil, errors.New("difftastic returned an out-of-range change span")
		}
		current := Range{Start: line.start + change.Start, End: line.start + change.End}
		if string(source[current.Start:current.End]) != change.Content {
			return nil, errors.New("difftastic change span did not match source; its JSON schema may have changed")
		}
		if current.End > current.Start {
			ranges = append(ranges, current)
		}
	}
	return ranges, nil
}

func sortRanges(ranges []Range) {
	for index := 1; index < len(ranges); index++ {
		for current := index; current > 0 && ranges[current].Start < ranges[current-1].Start; current-- {
			ranges[current], ranges[current-1] = ranges[current-1], ranges[current]
		}
	}
}
