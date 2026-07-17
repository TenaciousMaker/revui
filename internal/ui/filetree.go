package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/TenaciousMaker/revui/internal/diff"
)

type fileLayout uint8

const (
	flatFiles fileLayout = iota
	treeFiles
)

type fileScope uint8

const (
	changedFiles fileScope = iota
	contextFiles
	allRepositoryFiles
)

func (scope fileScope) label() string {
	switch scope {
	case contextFiles:
		return "CONTEXT"
	case allRepositoryFiles:
		return "ALL"
	default:
		return "CHANGED"
	}
}

type fileTreeNode struct {
	name      string
	path      string
	depth     int
	directory bool
	fileIndex int
	changed   bool
}

type fileTreeBranch struct {
	name      string
	path      string
	directory bool
	fileIndex int
	changed   bool
	children  map[string]*fileTreeBranch
}

type fileTreeEntry struct {
	path      string
	fileIndex int
}

func buildFileTree(files []diff.File, collapsed map[string]bool) []fileTreeNode {
	entries := make([]fileTreeEntry, 0, len(files))
	for fileIndex, file := range files {
		entries = append(entries, fileTreeEntry{path: file.Path, fileIndex: fileIndex})
	}
	return buildFileTreeEntries(entries, collapsed)
}

func buildFileTreeView(files []diff.File, allPaths []string, scope fileScope, collapsed map[string]bool) []fileTreeNode {
	return buildFileTreeEntries(scopedFileTreeEntries(files, allPaths, scope), collapsed)
}

func scopedFileTreeEntries(files []diff.File, allPaths []string, scope fileScope) []fileTreeEntry {
	changed := make(map[string]int, len(files))
	parents := make(map[string]bool, len(files))
	for index, file := range files {
		changed[file.Path] = index
		parents[path.Dir(file.Path)] = true
	}
	if scope == changedFiles {
		entries := make([]fileTreeEntry, 0, len(files))
		for index, file := range files {
			entries = append(entries, fileTreeEntry{path: file.Path, fileIndex: index})
		}
		return entries
	}

	paths := append([]string(nil), allPaths...)
	seen := make(map[string]bool, len(paths)+len(files))
	for _, candidate := range paths {
		seen[candidate] = true
	}
	for _, file := range files {
		if !seen[file.Path] {
			paths = append(paths, file.Path)
			seen[file.Path] = true
		}
	}
	sort.Slice(paths, func(i, j int) bool { return strings.ToLower(paths[i]) < strings.ToLower(paths[j]) })
	entries := make([]fileTreeEntry, 0, len(paths))
	for _, candidate := range paths {
		fileIndex, isChanged := changed[candidate]
		if scope == contextFiles && !isChanged && !parents[path.Dir(candidate)] {
			continue
		}
		if !isChanged {
			fileIndex = -1
		}
		entries = append(entries, fileTreeEntry{path: candidate, fileIndex: fileIndex})
	}
	return entries
}

func buildFileTreeEntries(entries []fileTreeEntry, collapsed map[string]bool) []fileTreeNode {
	root := &fileTreeBranch{directory: true, fileIndex: -1, children: map[string]*fileTreeBranch{}}
	for _, entry := range entries {
		parts := strings.Split(strings.Trim(entry.path, "/"), "/")
		branch := root
		if entry.fileIndex >= 0 {
			branch.changed = true
		}
		for i, part := range parts {
			if part == "" {
				continue
			}
			child, ok := branch.children[part]
			if !ok {
				path := part
				if branch.path != "" {
					path = branch.path + "/" + part
				}
				child = &fileTreeBranch{name: part, path: path, directory: i < len(parts)-1, fileIndex: -1, children: map[string]*fileTreeBranch{}}
				branch.children[part] = child
			}
			if i == len(parts)-1 {
				child.directory = false
				child.fileIndex = entry.fileIndex
			}
			if entry.fileIndex >= 0 {
				child.changed = true
			}
			branch = child
		}
	}

	var nodes []fileTreeNode
	var walk func(*fileTreeBranch, int)
	walk = func(branch *fileTreeBranch, depth int) {
		children := make([]*fileTreeBranch, 0, len(branch.children))
		for _, child := range branch.children {
			children = append(children, child)
		}
		sort.Slice(children, func(i, j int) bool {
			if children[i].directory != children[j].directory {
				return children[i].directory
			}
			return strings.ToLower(children[i].name) < strings.ToLower(children[j].name)
		})
		for _, child := range children {
			display, name := child, child.name
			if child.directory && !child.changed {
				for {
					only := onlyDirectoryChild(display)
					if only == nil || only.changed {
						break
					}
					display = only
					name += "/" + only.name
				}
			}
			nodes = append(nodes, fileTreeNode{name: name, path: display.path, depth: depth, directory: display.directory, fileIndex: display.fileIndex, changed: display.changed})
			if display.directory && !collapsed[display.path] {
				walk(display, depth+1)
			}
		}
	}
	walk(root, 0)
	return nodes
}

func onlyDirectoryChild(branch *fileTreeBranch) *fileTreeBranch {
	if len(branch.children) != 1 {
		return nil
	}
	for _, child := range branch.children {
		if child.directory {
			return child
		}
	}
	return nil
}

func fileReviewFingerprint(file diff.File) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%d\x00%d\n", file.Path, file.OldPath, file.Status, file.Additions, file.Deletions)
	for _, line := range file.Lines {
		_, _ = fmt.Fprintf(hash, "%d\x00%d\x00%d\x00%s\n", line.Kind, line.OldNumber, line.NewNumber, line.Text)
	}
	sum := hash.Sum(nil)
	return hex.EncodeToString(sum[:12])
}
