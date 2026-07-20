package ui

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode"
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
	provider  semanticProvider
	oldSource []byte
	newSource []byte
	plan      semantic.Plan
	err       error
}

type semanticProvider uint8

const (
	builtinSemantic semanticProvider = iota
	difftasticSemantic
)

type semanticAnalysisState struct {
	analyzer           semantic.Analyzer
	difftasticAnalyzer semantic.Analyzer
	cancel             context.CancelFunc
	id                 uint64
	repo               *gitrepo.Repository
	file               int
	provider           semanticProvider
	loading            bool
	ready              bool
	engine             semantic.Engine
	warning            string
	spans              map[int][]textSpan
	layout             *semantic.Layout
	alignment          []semantic.LineAlignment
	oldSource          []byte
	newSource          []byte
}

func (m Model) desiredSemanticProvider() semanticProvider {
	if m.difftasticMode {
		return difftasticSemantic
	}
	return builtinSemantic
}

func (m *Model) ensureSemanticAnalysis() tea.Cmd {
	if !m.semanticReflow || m.repo == nil || m.file < 0 || m.file >= len(m.repo.Files) || m.repo.Files[m.file].Binary {
		m.cancelSemanticAnalysis()
		return nil
	}
	provider := m.desiredSemanticProvider()
	if m.semantic.repo == m.repo && m.semantic.file == m.file && m.semantic.provider == provider && (m.semantic.loading || m.semantic.ready) {
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
	m.semantic.provider = provider
	m.semantic.loading = true
	m.semantic.ready = false
	m.semantic.engine = ""
	m.semantic.warning = ""
	m.semantic.spans = nil
	m.semantic.layout = nil
	m.semantic.alignment = nil
	m.semantic.oldSource = nil
	m.semantic.newSource = nil
	id, repo, file := m.semantic.id, m.repo, m.file
	operations, analyzer := m.repositories, m.semantic.analyzer
	if provider == difftasticSemantic {
		analyzer = m.semantic.difftasticAnalyzer
	}
	return func() tea.Msg {
		oldSource, newSource, err := operations.ReadPair(ctx, repo, repo.Files[file])
		if err != nil {
			return semanticResultMsg{id: id, repo: repo, file: file, provider: provider, err: err}
		}
		if analyzer == nil {
			return semanticResultMsg{id: id, repo: repo, file: file, provider: provider, err: errors.New("semantic analyzer is unavailable")}
		}
		plan, err := analyzer.Analyze(ctx, semantic.Input{Path: repo.Files[file].Path, Old: oldSource, New: newSource})
		return semanticResultMsg{
			id: id, repo: repo, file: file, provider: provider, oldSource: oldSource, newSource: newSource, plan: plan, err: err,
		}
	}
}

func (m *Model) applySemanticResult(msg semanticResultMsg) bool {
	if !m.semanticReflow || msg.id != m.semantic.id || msg.repo != m.repo || msg.file != m.file ||
		msg.repo != m.semantic.repo || msg.file != m.semantic.file || msg.provider != m.desiredSemanticProvider() || msg.provider != m.semantic.provider {
		return false
	}
	position := m.captureSplitLayoutPosition()
	m.semantic.cancel = nil
	m.semantic.loading = false
	if msg.err != nil {
		m.semantic.ready = false
		m.semantic.warning = msg.err.Error()
		if msg.provider == difftasticSemantic {
			m.status = "Difftastic unavailable: " + msg.err.Error() + ". Showing raw split diff."
		} else {
			m.status = "Semantic analysis unavailable: " + msg.err.Error()
		}
		return false
	}
	m.semantic.ready = true
	m.semantic.engine = msg.plan.Engine
	m.semantic.warning = msg.plan.Warning
	rawSpans := projectSemanticPlan(m.repo.Files[m.file].Lines, msg.plan, msg.oldSource, msg.newSource)
	if msg.provider == difftasticSemantic {
		m.semantic.spans = rawSpans
	} else {
		m.semantic.spans = refineSemanticSpans(m.repo.Files[m.file].Lines, filterSemanticSpans(m.repo.Files[m.file].Lines, rawSpans))
	}
	m.semantic.layout = msg.plan.Layout
	m.semantic.alignment = msg.plan.Alignment
	m.semantic.oldSource = msg.oldSource
	m.semantic.newSource = msg.newSource
	if msg.provider == difftasticSemantic && len(m.semantic.alignment) > 0 {
		if _, complete := buildDifftasticSplitRows(m.currentLines(), m.semantic.alignment, msg.oldSource, msg.newSource, m.semantic.spans); !complete {
			m.semantic.alignment = nil
			m.semantic.warning = "Difftastic alignment did not account for every visible Git row"
		}
	}
	m.restoreSplitLayoutPosition(position)
	if msg.provider == difftasticSemantic && m.semantic.warning != "" {
		m.status = m.semantic.warning + "; showing the raw split diff."
	} else if msg.provider == difftasticSemantic && len(m.semantic.alignment) > 0 {
		m.status = "Difftastic alignment ready."
	} else if msg.provider == difftasticSemantic {
		m.status = "Difftastic returned no line alignment; showing the raw split diff."
	} else if msg.plan.Warning != "" {
		m.status = msg.plan.Warning
	} else if m.normalizedLayout && msg.plan.Layout != nil {
		m.status = "Normalized split ready."
	} else if m.normalizedLayout {
		m.status = "Normalized layout unavailable; showing the raw split diff."
	} else if msg.plan.Engine == semantic.EngineAST {
		m.status = "AST highlighting ready."
	} else {
		m.status = "Token highlighting ready; this language has no AST adapter."
	}
	return (msg.provider == difftasticSemantic && len(m.semantic.alignment) > 0) ||
		(m.normalizedLayout && msg.plan.Layout != nil)
}

// Tree-sitter intentionally treats string literals as atomic syntax. When a
// one-line replacement changes only part of such a leaf (class lists are the
// common case), refine an otherwise-empty AST result with the lexical matcher.
// Restricting this to one-for-one replacement runs avoids reintroducing dense
// emphasis across formatter-driven multi-line rewrites.
func refineSemanticSpans(lines []diff.Line, spans map[int][]textSpan) map[int][]textSpan {
	for start := 0; start < len(lines); {
		if !isChangedLine(lines[start]) {
			start++
			continue
		}
		hunk := lines[start].Hunk
		end := start
		var deletions, additions []int
		for end < len(lines) && lines[end].Hunk == hunk && isChangedLine(lines[end]) {
			if lines[end].Kind == diff.Deletion {
				deletions = append(deletions, end)
			} else {
				additions = append(additions, end)
			}
			end++
		}
		if len(deletions) == 1 && len(additions) == 1 && len(spans[deletions[0]]) == 0 && len(spans[additions[0]]) == 0 {
			oldText, newText := expandTabs(lines[deletions[0]].Text), expandTabs(lines[additions[0]].Text)
			oldSpans, newSpans := intralineChanges(oldText, newText)
			if usefulSemanticEmphasis(oldText, oldSpans) {
				spans[deletions[0]] = oldSpans
			}
			if usefulSemanticEmphasis(newText, newSpans) {
				spans[additions[0]] = newSpans
			}
		}
		start = end
	}
	return spans
}

func filterSemanticSpans(lines []diff.Line, spans map[int][]textSpan) map[int][]textSpan {
	eligible := make([]bool, len(lines))
	for start := 0; start < len(lines); {
		if lines[start].Kind != diff.Deletion && lines[start].Kind != diff.Addition {
			start++
			continue
		}
		end, hasDeletion, hasAddition := start, false, false
		for end < len(lines) && (lines[end].Kind == diff.Deletion || lines[end].Kind == diff.Addition) && lines[end].Hunk == lines[start].Hunk {
			hasDeletion = hasDeletion || lines[end].Kind == diff.Deletion
			hasAddition = hasAddition || lines[end].Kind == diff.Addition
			end++
		}
		if hasDeletion && hasAddition {
			for index := start; index < end; index++ {
				eligible[index] = true
			}
		}
		start = end
	}
	filtered := make(map[int][]textSpan)
	for index, lineSpans := range spans {
		if index < 0 || index >= len(lines) || !eligible[index] || !usefulSemanticEmphasis(lines[index].Text, lineSpans) {
			continue
		}
		filtered[index] = lineSpans
	}
	return filtered
}

func usefulSemanticEmphasis(text string, spans []textSpan) bool {
	runes := []rune(expandTabs(text))
	emphasized := make([]bool, len(runes))
	for _, span := range spans {
		for index := max(0, span.start); index < min(len(runes), span.end); index++ {
			emphasized[index] = true
		}
	}
	meaningful, covered := 0, 0
	for index, value := range runes {
		if unicode.IsSpace(value) {
			continue
		}
		meaningful++
		if emphasized[index] {
			covered++
		}
	}
	return meaningful > 0 && covered > 0 && (covered*100 < meaningful*65 || standaloneSemanticItem(text))
}

// A formatter commonly turns one import/argument/member per line into a
// single compact line. In that case a removed identifier legitimately covers
// almost the entire old visual line, but it is still the one fact the reviewer
// needs to see. Keep that emphasis without allowing dense statements back in.
func standaloneSemanticItem(text string) bool {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(strings.TrimSuffix(text, ","), ";")
	if text == "" {
		return false
	}
	for index, value := range text {
		if unicode.IsLetter(value) || unicode.IsDigit(value) || value == '_' || value == '$' || value == '.' {
			if index == 0 && unicode.IsDigit(value) {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func (m Model) normalizedLayoutReady() bool {
	return m.semantic.ready && m.semantic.repo == m.repo && m.semantic.file == m.file && m.semantic.layout != nil
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
	m.semantic.layout = nil
	m.semantic.alignment = nil
	m.semantic.oldSource = nil
	m.semantic.newSource = nil
}

func (m Model) semanticLabel() string {
	if !m.semanticReflow {
		return ""
	}
	if m.repo == nil || m.file < 0 || m.file >= len(m.repo.Files) || m.repo.Files[m.file].Binary {
		if m.difftasticMode {
			return "DIFFT N/A"
		}
		return "SEM N/A"
	}
	if m.semantic.repo != m.repo || m.semantic.file != m.file || m.semantic.provider != m.desiredSemanticProvider() {
		if m.difftasticMode {
			return "DIFFT…"
		}
		return "SEM…"
	}
	if m.semantic.loading {
		if m.difftasticMode {
			return "DIFFT…"
		}
		return "SEM…"
	}
	if m.semantic.ready {
		if m.difftasticMode && len(m.semantic.alignment) == 0 {
			return "DIFFT!"
		}
		return string(m.semantic.engine)
	}
	if m.semantic.warning != "" {
		if m.difftasticMode {
			return "DIFFT!"
		}
		return "SEM!"
	}
	if m.difftasticMode {
		return "DIFFT*"
	}
	return "TOKEN*"
}

func (m Model) normalizationLabel() string {
	if !m.normalizedLayout {
		return ""
	}
	if m.semantic.loading || m.semantic.repo != m.repo || m.semantic.file != m.file {
		return "NORM…"
	}
	if m.semantic.ready && m.semantic.layout != nil {
		return "NORMALIZED"
	}
	return "NORM N/A"
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
	oldRanges := plan.ChangedRanges(semantic.OldSide)
	newRanges := plan.ChangedRanges(semantic.NewSide)
	result := make(map[int][]textSpan)
	for index, line := range lines {
		var source []byte
		var starts []int
		var ranges []semantic.Range
		var number int
		switch line.Kind {
		case diff.Deletion:
			source, starts, ranges, number = oldSource, oldStarts, oldRanges, line.OldNumber
		case diff.Addition:
			source, starts, ranges, number = newSource, newStarts, newRanges, line.NewNumber
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
