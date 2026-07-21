package ui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
	"github.com/TenaciousMaker/revui/internal/review"
)

// reviewFileOperations keeps working-tree reads and Git comparisons outside
// the interaction model. Both operations run in cancellable Bubble Tea cmds.
type reviewFileOperations interface {
	Capture(context.Context, *gitrepo.Repository, diff.File) (gitrepo.SourceSnapshot, error)
	Compare(context.Context, *gitrepo.Repository, diff.File, gitrepo.SourceSnapshot) (diff.File, gitrepo.SourceSnapshot, error)
}

type gitReviewFileOperations struct{}

func (gitReviewFileOperations) Capture(ctx context.Context, repo *gitrepo.Repository, file diff.File) (gitrepo.SourceSnapshot, error) {
	return repo.CaptureReviewSourceContext(ctx, file)
}

func (gitReviewFileOperations) Compare(ctx context.Context, repo *gitrepo.Repository, file diff.File, baseline gitrepo.SourceSnapshot) (diff.File, gitrepo.SourceSnapshot, error) {
	return repo.DiffFromReviewContext(ctx, file, baseline)
}

type capturedReview struct {
	path        string
	fingerprint string
	source      gitrepo.SourceSnapshot
	err         error
}

type reviewCaptureMsg struct {
	id       uint64
	repo     *gitrepo.Repository
	all      bool
	captures []capturedReview
}

type reviewComparisonMsg struct {
	id      uint64
	repo    *gitrepo.Repository
	file    int
	path    string
	delta   diff.File
	current gitrepo.SourceSnapshot
	err     error
}

// reviewInteractionState owns only transient work. Durable review state lives
// in review.Session, and the repository remains the canonical branch snapshot.
type reviewInteractionState struct {
	files reviewFileOperations

	captureCancel  context.CancelFunc
	captureID      uint64
	captureLoading bool

	comparisonCancel  context.CancelFunc
	comparisonID      uint64
	comparisonLoading bool
	comparisonRepo    *gitrepo.Repository
	comparisonFile    int
	comparisonPath    string
	comparison        *diff.File
	comparisonBefore  gitrepo.SourceSnapshot
	comparisonCurrent gitrepo.SourceSnapshot
}

func newReviewInteractionState() reviewInteractionState {
	return reviewInteractionState{files: gitReviewFileOperations{}, comparisonFile: -1}
}

func (m Model) selectedChangedFileIndex() int {
	if m.fileLayout == treeFiles && m.focus == focusFiles {
		nodes := m.currentTreeNodes()
		if m.treeCursor >= 0 && m.treeCursor < len(nodes) && !nodes[m.treeCursor].directory {
			return nodes[m.treeCursor].fileIndex
		}
	}
	if m.sourcePath != "" {
		if index, ok := m.changedFileIndex(m.sourcePath); ok {
			return index
		}
	}
	if m.file >= 0 && m.file < len(m.repo.Files) {
		return m.file
	}
	return -1
}

func (m *Model) toggleReviewedFile() tea.Cmd {
	fileIndex := m.selectedChangedFileIndex()
	if fileIndex < 0 || fileIndex >= len(m.repo.Files) {
		m.status = "Only changed files can be marked reviewed."
		return nil
	}
	file := m.repo.Files[fileIndex]
	if m.session.IsReviewed(file.Path, fileReviewFingerprint(file)) {
		m.session.Unreview(file.Path)
		m.clearReviewComparison()
		m.persist()
		m.status = "Marked " + file.Path + " as unreviewed."
		return nil
	}
	return m.captureReviews([]int{fileIndex}, false)
}

func (m *Model) toggleAllReviewed() tea.Cmd {
	if len(m.repo.Files) == 0 {
		m.status = "No changed files to mark reviewed."
		return nil
	}
	allCurrent := true
	for index := range m.repo.Files {
		if !m.fileReviewed(index) {
			allCurrent = false
			break
		}
	}
	if allCurrent {
		for _, file := range m.repo.Files {
			m.session.Unreview(file.Path)
		}
		m.clearReviewComparison()
		m.persist()
		m.status = fmt.Sprintf("Marked all %d changed files as unreviewed.", len(m.repo.Files))
		return nil
	}
	indices := make([]int, len(m.repo.Files))
	for index := range indices {
		indices[index] = index
	}
	return m.captureReviews(indices, true)
}

func (m *Model) captureReviews(indices []int, all bool) tea.Cmd {
	if m.reviewWork.captureCancel != nil {
		m.reviewWork.captureCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.reviewWork.captureCancel = cancel
	m.reviewWork.captureID++
	m.reviewWork.captureLoading = true
	id, repo, operations := m.reviewWork.captureID, m.repo, m.reviewWork.files
	files := make([]diff.File, 0, len(indices))
	for _, index := range indices {
		if index >= 0 && index < len(repo.Files) {
			files = append(files, repo.Files[index])
		}
	}
	if all {
		m.status = fmt.Sprintf("Capturing review baselines for %d files…", len(files))
	} else if len(files) > 0 {
		m.status = "Capturing review baseline for " + files[0].Path + "…"
	}
	return func() tea.Msg {
		captures := make([]capturedReview, 0, len(files))
		for _, file := range files {
			source, err := operations.Capture(ctx, repo, file)
			captures = append(captures, capturedReview{
				path: file.Path, fingerprint: fileReviewFingerprint(file), source: source, err: err,
			})
			if ctx.Err() != nil {
				break
			}
		}
		return reviewCaptureMsg{id: id, repo: repo, all: all, captures: captures}
	}
}

func (m *Model) applyReviewCapture(msg reviewCaptureMsg) {
	if msg.id != m.reviewWork.captureID || msg.repo != m.repo {
		return
	}
	m.reviewWork.captureCancel = nil
	m.reviewWork.captureLoading = false
	failed := 0
	for _, capture := range msg.captures {
		if capture.err != nil {
			failed++
			// Preserve the useful reviewed toggle even if a file disappears in
			// the instant between a refresh and baseline capture.
			m.session.SetReviewed(capture.path, capture.fingerprint, nil, capture.source.Exists)
			continue
		}
		var source []byte
		if capture.source.Available {
			source = capture.source.Content
		}
		m.session.SetReviewed(capture.path, capture.fingerprint, source, capture.source.Exists)
	}
	m.clearReviewComparison()
	m.persist()
	if msg.all {
		m.status = fmt.Sprintf("Marked %d changed files as reviewed.", len(msg.captures))
	} else if len(msg.captures) > 0 {
		m.status = "Marked " + msg.captures[0].path + " as reviewed."
	}
	if failed > 0 {
		m.status += fmt.Sprintf(" %d baseline snapshot(s) unavailable.", failed)
	}
}

func (m *Model) toggleReviewComparison() tea.Cmd {
	fileIndex := m.selectedChangedFileIndex()
	if fileIndex < 0 || fileIndex >= len(m.repo.Files) {
		m.status = "Select a changed file to compare with its last review."
		return nil
	}
	file := m.repo.Files[fileIndex]
	if m.reviewComparisonActive() || m.reviewComparisonLoadingFor(fileIndex, file.Path) {
		m.clearReviewComparison()
		m.resetLineCursor()
		m.status = "Showing the branch diff."
		return m.ensureSemanticAnalysis()
	}
	status := m.session.Status(file.Path, fileReviewFingerprint(file))
	switch status {
	case review.Unreviewed:
		m.status = "Mark this file reviewed before comparing later changes."
		return nil
	case review.Reviewed:
		m.status = "This file has not changed since it was reviewed."
		return nil
	}
	baseline, ok := m.session.Baseline(file.Path, fileReviewFingerprint(file))
	if !ok {
		m.status = "No text baseline is available. Mark the file reviewed again to capture one."
		return nil
	}
	if m.reviewWork.comparisonCancel != nil {
		m.reviewWork.comparisonCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.reviewWork.comparisonCancel = cancel
	m.reviewWork.comparisonID++
	m.reviewWork.comparisonLoading = true
	m.reviewWork.comparisonRepo = m.repo
	m.reviewWork.comparisonFile = fileIndex
	m.reviewWork.comparisonPath = file.Path
	id, repo, operations := m.reviewWork.comparisonID, m.repo, m.reviewWork.files
	snapshot := gitrepo.SourceSnapshot{Content: baseline.Source, Exists: baseline.Exists, Available: true}
	m.reviewWork.comparisonBefore = snapshot
	m.cancelSemanticAnalysis()
	m.status = "Comparing " + file.Path + " with its last reviewed version…"
	return func() tea.Msg {
		delta, current, err := operations.Compare(ctx, repo, file, snapshot)
		return reviewComparisonMsg{id: id, repo: repo, file: fileIndex, path: file.Path, delta: delta, current: current, err: err}
	}
}

func (m *Model) applyReviewComparison(msg reviewComparisonMsg) {
	if msg.id != m.reviewWork.comparisonID || msg.repo != m.repo || msg.file != m.file || msg.path != m.currentPath() {
		return
	}
	m.reviewWork.comparisonCancel = nil
	m.reviewWork.comparisonLoading = false
	if msg.err != nil {
		m.status = "Compare with last review failed: " + msg.err.Error()
		return
	}
	m.reviewWork.comparisonRepo = msg.repo
	m.reviewWork.comparisonFile = msg.file
	m.reviewWork.comparisonPath = msg.path
	m.reviewWork.comparison = &msg.delta
	m.reviewWork.comparisonCurrent = msg.current
	m.resetLineCursor()
	if len(msg.delta.Lines) == 0 {
		m.status = "No textual changes since the last review."
	} else {
		m.status = "Showing changes since the last review. Press u to return to the branch diff."
	}
}

func (m Model) reviewComparisonActive() bool {
	return m.reviewWork.comparison != nil && m.reviewWork.comparisonRepo == m.repo &&
		m.reviewWork.comparisonFile == m.file && m.reviewWork.comparisonPath == m.currentPath()
}

func (m Model) reviewComparisonLoadingFor(file int, path string) bool {
	return m.reviewWork.comparisonLoading && m.reviewWork.comparisonRepo == m.repo &&
		m.reviewWork.comparisonFile == file && m.reviewWork.comparisonPath == path
}

func (m *Model) clearReviewComparison() {
	if m.reviewWork.comparisonCancel != nil {
		m.reviewWork.comparisonCancel()
	}
	m.reviewWork.comparisonCancel = nil
	m.reviewWork.comparisonLoading = false
	m.reviewWork.comparisonRepo = nil
	m.reviewWork.comparisonFile = -1
	m.reviewWork.comparisonPath = ""
	m.reviewWork.comparison = nil
	m.reviewWork.comparisonBefore = gitrepo.SourceSnapshot{}
	m.reviewWork.comparisonCurrent = gitrepo.SourceSnapshot{}
}

func (m *Model) cancelReviewWork() {
	if m.reviewWork.captureCancel != nil {
		m.reviewWork.captureCancel()
	}
	m.reviewWork.captureCancel = nil
	m.reviewWork.captureLoading = false
	m.clearReviewComparison()
}
