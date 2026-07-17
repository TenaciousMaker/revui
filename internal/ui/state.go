package ui

import (
	"time"

	"github.com/TenaciousMaker/revui/internal/gitrepo"
)

// filePaneState owns navigation and layout for the repository explorer.
type filePaneState struct {
	file               int
	fileScroll         int
	fileLayout         fileLayout
	fileScope          fileScope
	wideFiles          bool
	treeCursor         int
	treeNodes          []fileTreeNode
	treeNodesReady     bool
	treeNodesScope     fileScope
	treeNodesFileCount int
	treeNodesPathCount int
	treeFileCount      int
	treeRequiredWidth  int
	additionsWidth     int
	deletionsWidth     int
	collapsed          map[string]bool
}

// contentPaneState owns diff and full-source navigation.
type contentPaneState struct {
	view             viewMode
	line             int
	lineScroll       int
	splitCursor      int
	splitScroll      int
	selectFrom       int
	sourcePath       string
	sourceLines      []string
	sourceLine       int
	sourceScroll     int
	sourceFromBase   bool
	ignoreWhitespace bool
	ignoreMoved      bool
	semanticReflow   bool
}

// searchState owns both changed-file fuzzy search and repository text search.
type searchState struct {
	input           string
	inputCursor     int
	searchHits      []int
	searchAt        int
	searchTop       int
	repoHits        []gitrepo.SearchMatch
	repoSearchAt    int
	repoSearchTop   int
	repoSearching   bool
	repoSearchReady bool
}

// selectionState owns terminal mouse selection independently of navigation.
type selectionState struct {
	mouseSelecting   bool
	mouseSelectMoved bool
	mouseSelectStart mousePoint
	mouseSelectEnd   mousePoint
	mouseSelectLeft  int
	mouseSelectRight int
	selectedText     string
}

// viewportState owns focus, dimensions, and accelerated wheel input.
type viewportState struct {
	width              int
	height             int
	focus              focusArea
	wheelScheduled     bool
	wheelDirection     int
	wheelBurst         int
	wheelTarget        wheelPane
	wheelVelocity      float64
	wheelLastAt        time.Time
	wheelLastDirection int
	wheelLastTarget    wheelPane
	now                func() time.Time
}
