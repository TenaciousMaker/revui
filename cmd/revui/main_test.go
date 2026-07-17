package main

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TenaciousMaker/revui/internal/gitrepo"
	"github.com/TenaciousMaker/revui/internal/ui"
)

func TestRunHelpVersionAndUsageErrors(t *testing.T) {
	for _, test := range []struct {
		args     []string
		wantCode int
		want     string
	}{
		{[]string{"--help"}, 0, "Usage:"},
		{[]string{"--version"}, 0, "revui"},
		{[]string{"unexpected"}, 2, "unexpected argument"},
		{[]string{"--unknown"}, 2, "flag provided but not defined"},
	} {
		var stdout, stderr bytes.Buffer
		code := run(test.args, strings.NewReader(""), &stdout, &stderr)
		if code != test.wantCode || !strings.Contains(stdout.String()+stderr.String(), test.want) {
			t.Fatalf("run(%v) code=%d output=%q", test.args, code, stdout.String()+stderr.String())
		}
	}
}

func TestRunReportsRepositoryFailure(t *testing.T) {
	originalGetwd, originalOpen := workingDirectory, openRepository
	t.Cleanup(func() { workingDirectory, openRepository = originalGetwd, originalOpen })
	workingDirectory = func() (string, error) { return "/repo", nil }
	openRepository = func(string, string) (*gitrepo.Repository, error) { return nil, errors.New("not a repository") }
	var stderr bytes.Buffer
	if code := run(nil, strings.NewReader(""), io.Discard, &stderr); code != 1 || !strings.Contains(stderr.String(), "not a repository") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunWiresBaseAndProgram(t *testing.T) {
	originalGetwd, originalOpen, originalRun := workingDirectory, openRepository, runProgram
	t.Cleanup(func() { workingDirectory, openRepository, runProgram = originalGetwd, originalOpen, originalRun })
	root := t.TempDir()
	workingDirectory = func() (string, error) { return root, nil }
	var gotStart, gotBase string
	openRepository = func(start, base string) (*gitrepo.Repository, error) {
		gotStart, gotBase = start, base
		return &gitrepo.Repository{
			Root: root, Branch: "feature", Base: base,
			ReviewPath:      filepath.Join(root, ".git", "revui", "feature.json"),
			PreferencesPath: filepath.Join(root, "preferences.json"),
		}, nil
	}
	called := false
	runProgram = func(ui.Model, io.Reader, io.Writer) error { called = true; return nil }
	if code := run([]string{"--base", "origin/main"}, strings.NewReader(""), io.Discard, io.Discard); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if gotStart != root || gotBase != "origin/main" || !called {
		t.Fatalf("start=%q base=%q called=%v", gotStart, gotBase, called)
	}
}

func TestRunReportsWorkingDirectoryAndProgramFailures(t *testing.T) {
	originalGetwd, originalOpen, originalRun := workingDirectory, openRepository, runProgram
	t.Cleanup(func() { workingDirectory, openRepository, runProgram = originalGetwd, originalOpen, originalRun })
	workingDirectory = func() (string, error) { return "", errors.New("cwd unavailable") }
	var stderr bytes.Buffer
	if code := run(nil, strings.NewReader(""), io.Discard, &stderr); code != 1 || !strings.Contains(stderr.String(), "cwd unavailable") {
		t.Fatalf("cwd failure code=%d stderr=%q", code, stderr.String())
	}

	root := t.TempDir()
	workingDirectory = func() (string, error) { return root, nil }
	openRepository = func(string, string) (*gitrepo.Repository, error) {
		return &gitrepo.Repository{
			Root: root, Branch: "feature", Base: "main",
			ReviewPath: filepath.Join(root, "review.json"), PreferencesPath: filepath.Join(root, "preferences.json"),
		}, nil
	}
	runProgram = func(ui.Model, io.Reader, io.Writer) error { return errors.New("terminal unavailable") }
	stderr.Reset()
	if code := run(nil, strings.NewReader(""), io.Discard, &stderr); code != 1 || !strings.Contains(stderr.String(), "terminal unavailable") {
		t.Fatalf("program failure code=%d stderr=%q", code, stderr.String())
	}
}
