package ui

import (
	"context"
	"sort"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
	"github.com/TenaciousMaker/revui/internal/semantic"
)

type semanticResultMsg struct {
	id        uint64
	repo      *gitrepo.Repository
	file      int
	oldSource []byte
	newSource []byte
	plan      semantic.Plan
	err       error
}

type semanticAnalysisState struct {
	analyzer semantic.Analyzer
	cancel   context.CancelFunc
	id       uint64
	repo     *gitrepo.Repository
	file     int
	loading  bool
	ready    bool
	engine   semantic.Engine
	warning  string
	spans    map[int][]textSpan
}

func (m *Model) ensureSemanticAnalysis() tea.Cmd {
	if !m.semanticReflow || m.repo == nil || m.file < 0 || m.file >= len(m.repo.Files) || m.repo.Files[m.file].Binary {
		m.cancelSemanticAnalysis()
		return nil
	}
	if m.semantic.repo == m.repo && m.semantic.file == m.file && (m.semantic.loading || m.semantic.ready) {
		return nil
	}
	if m.semantic.cancel != nil {
		m.semantic.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.semantic.cancel = cancel
	m.semantic.id++
	m.semantic.repo = m.repo
	m.semantic.file = m.file
	m.semantic.loading = true
	m.semantic.ready = false
	m.semantic.engine = ""
	m.semantic.warning = ""
	m.semantic.spans = nil
	id, repo, file := m.semantic.id, m.repo, m.file
	operations, analyzer := m.repositories, m.semantic.analyzer
	return func() tea.Msg {
		oldSource, newSource, err := operations.ReadPair(ctx, repo, repo.Files[file])
		if err != nil {
			return semanticResultMsg{id: id, repo: repo, file: file, err: err}
		}
		plan, err := analyzer.Analyze(ctx, semantic.Input{Path: repo.Files[file].Path, Old: oldSource, New: newSource})
		return semanticResultMsg{
			id: id, repo: repo, file: file, oldSource: oldSource, newSource: newSource, plan: plan, err: err,
		}
	}
}

func (m *Model) applySemanticResult(msg semanticResultMsg) {
	if !m.semanticReflow || msg.id != m.semantic.id || msg.repo != m.repo || msg.file != m.file ||
		msg.repo != m.semantic.repo || msg.file != m.semantic.file {
		return
	}
	m.semantic.cancel = nil
	m.semantic.loading = false
	if msg.err != nil {
		m.semantic.ready = false
		m.semantic.warning = msg.err.Error()
		m.status = "Semantic analysis unavailable: " + msg.err.Error()
		return
	}
	m.semantic.ready = true
	m.semantic.engine = msg.plan.Engine
	m.semantic.warning = msg.plan.Warning
	m.semantic.spans = projectSemanticPlan(m.repo.Files[m.file].Lines, msg.plan, msg.oldSource, msg.newSource)
	if msg.plan.Warning != "" {
		m.status = msg.plan.Warning
	} else if msg.plan.Engine == semantic.EngineAST {
		m.status = "AST highlighting ready."
	} else {
		m.status = "Token highlighting ready; this language has no AST adapter."
	}
}

func (m *Model) cancelSemanticAnalysis() {
	if m.semantic.cancel != nil {
		m.semantic.cancel()
		m.semantic.cancel = nil
	}
	m.semantic.loading = false
	m.semantic.repo = nil
	m.semantic.file = -1
	m.semantic.ready = false
	m.semantic.spans = nil
	m.semantic.engine = ""
	m.semantic.warning = ""
}

func (m Model) semanticLabel() string {
	if !m.semanticReflow {
		return ""
	}
	if m.repo == nil || m.file < 0 || m.file >= len(m.repo.Files) || m.repo.Files[m.file].Binary {
		return "SEM N/A"
	}
	if m.semantic.repo != m.repo || m.semantic.file != m.file {
		return "SEM…"
	}
	if m.semantic.loading {
		return "SEM…"
	}
	if m.semantic.ready {
		return string(m.semantic.engine)
	}
	if m.semantic.warning != "" {
		return "SEM!"
	}
	return "TOKEN*"
}

func (m Model) semanticSpansForVisibleLines(lines []diff.Line) map[int][]textSpan {
	if !m.semanticReflow || !m.semantic.ready || m.semantic.repo != m.repo || m.semantic.file != m.file {
		return nil
	}
	visible := make(map[int][]textSpan)
	for index, line := range lines {
		if spans := m.semantic.spans[line.OriginalIndex]; len(spans) > 0 {
			visible[index] = spans
		}
	}
	return visible
}

func projectSemanticPlan(lines []diff.Line, plan semantic.Plan, oldSource, newSource []byte) map[int][]textSpan {
	oldStarts := sourceLineStarts(oldSource)
	newStarts := sourceLineStarts(newSource)
	result := make(map[int][]textSpan)
	for index, line := range lines {
		var source []byte
		var starts []int
		var ranges []semantic.Range
		var number int
		switch line.Kind {
		case diff.Deletion:
			source, starts, ranges, number = oldSource, oldStarts, plan.Old, line.OldNumber
		case diff.Addition:
			source, starts, ranges, number = newSource, newStarts, plan.New, line.NewNumber
		default:
			continue
		}
		if number <= 0 || number > len(starts) {
			continue
		}
		lineStart := starts[number-1]
		lineEnd := len(source)
		if number < len(starts) {
			lineEnd = starts[number] - 1
			if lineEnd > lineStart && source[lineEnd-1] == '\r' {
				lineEnd--
			}
		}
		first := sort.Search(len(ranges), func(index int) bool { return ranges[index].End > lineStart })
		for _, changed := range ranges[first:] {
			if changed.Start >= lineEnd {
				break
			}
			start, end := max(changed.Start, lineStart), min(changed.End, lineEnd)
			if start >= end {
				continue
			}
			localStart := expandedRuneOffset(source[lineStart:start])
			localEnd := expandedRuneOffset(source[lineStart:end])
			result[index] = append(result[index], textSpan{start: localStart, end: localEnd})
		}
	}
	return result
}

func sourceLineStarts(source []byte) []int {
	starts := []int{0}
	for index, value := range source {
		if value == '\n' && index+1 < len(source) {
			starts = append(starts, index+1)
		}
	}
	return starts
}

func expandedRuneOffset(source []byte) int {
	result := 0
	for len(source) > 0 {
		value, size := utf8.DecodeRune(source)
		if value == '\t' {
			result += 4
		} else {
			result++
		}
		source = source[size:]
	}
	return result
}
