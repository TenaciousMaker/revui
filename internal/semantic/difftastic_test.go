package semantic

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type difftasticRunnerFunc func(context.Context, string, string) ([]byte, error)

func (f difftasticRunnerFunc) Run(ctx context.Context, oldPath, newPath string) ([]byte, error) {
	return f(ctx, oldPath, newPath)
}

func TestDifftasticAnalyzerProjectsTokenSpansAndLineAlignment(t *testing.T) {
	oldSource := []byte("const café = \"old\";\n")
	newSource := []byte("const café = \"new\";\n")
	var temporary string
	runner := difftasticRunnerFunc(func(_ context.Context, oldPath, newPath string) ([]byte, error) {
		temporary = filepath.Dir(filepath.Dir(oldPath))
		oldOnDisk, err := os.ReadFile(oldPath)
		if err != nil {
			t.Fatal(err)
		}
		newOnDisk, err := os.ReadFile(newPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(oldOnDisk) != string(oldSource) || string(newOnDisk) != string(newSource) {
			t.Fatalf("temporary source mismatch: %q | %q", oldOnDisk, newOnDisk)
		}
		return []byte(`{"aligned_lines":[[0,0],[1,1]],"chunks":[[{"lhs":{"line_number":0,"changes":[{"start":14,"end":19,"content":"\"old\""}]},"rhs":{"line_number":0,"changes":[{"start":14,"end":19,"content":"\"new\""}]}}]],"language":"TypeScript","status":"changed"}`), nil
	})
	plan, err := newDifftasticAnalyzer(0, runner).Analyze(context.Background(), Input{Path: "value.ts", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Engine != EngineDifftastic {
		t.Fatalf("engine = %q, want %q", plan.Engine, EngineDifftastic)
	}
	if len(plan.Alignment) != 1 || plan.Alignment[0] != (LineAlignment{Old: 1, New: 1}) {
		t.Fatalf("alignment = %#v", plan.Alignment)
	}
	if got := selected(oldSource, plan.ChangedRanges(OldSide)); got != `"old"` {
		t.Fatalf("old changed text = %q", got)
	}
	if got := selected(newSource, plan.ChangedRanges(NewSide)); got != `"new"` {
		t.Fatalf("new changed text = %q", got)
	}
	if _, err := os.Stat(temporary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary Difftastic workspace still exists: %v", err)
	}
}

func TestDifftasticAnalyzerAcceptsArrayJSONAndCachesByContent(t *testing.T) {
	calls := 0
	runner := difftasticRunnerFunc(func(context.Context, string, string) ([]byte, error) {
		calls++
		return []byte(`[{"aligned_lines":[[0,0]],"chunks":[],"language":"Text","status":"unchanged"}]`), nil
	})
	analyzer := newDifftasticAnalyzer(1, runner)
	input := Input{Path: "notes.txt", Old: []byte("same"), New: []byte("same")}
	for range 2 {
		if _, err := analyzer.Analyze(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("difftastic calls = %d, want 1", calls)
	}
}

func TestDifftasticAnalyzerRejectsSchemaDrift(t *testing.T) {
	runner := difftasticRunnerFunc(func(context.Context, string, string) ([]byte, error) {
		return []byte(`{"aligned_lines":[[0,0]],"chunks":[[{"lhs":{"line_number":0,"changes":[{"start":0,"end":4,"content":"nope"}]}}]]}`), nil
	})
	_, err := newDifftasticAnalyzer(0, runner).Analyze(context.Background(), Input{Path: "value.ts", Old: []byte("same"), New: []byte("same")})
	if err == nil || !strings.Contains(err.Error(), "schema may have changed") {
		t.Fatalf("schema error = %v", err)
	}
}

func TestDifftasticAnalyzerCancellation(t *testing.T) {
	runner := difftasticRunnerFunc(func(ctx context.Context, _, _ string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newDifftasticAnalyzer(0, runner).Analyze(ctx, Input{Path: "value.ts", Old: []byte("old"), New: []byte("new")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestInstalledDifftasticIntegration(t *testing.T) {
	if _, err := exec.LookPath("difft"); err != nil {
		t.Skip("difft is not installed")
	}
	oldSource := []byte("import {\n  useEffect,\n  useLayoutEffect,\n  useState,\n} from 'react';\n")
	newSource := []byte("import { useEffect, useState } from 'react';\n")
	plan, err := NewDifftastic(0).Analyze(context.Background(), Input{Path: "component.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Engine != EngineDifftastic || len(plan.Alignment) == 0 {
		t.Fatalf("installed Difftastic plan = %#v", plan)
	}
	if removed := selected(oldSource, plan.ChangedRanges(OldSide)); !strings.Contains(removed, "useLayoutEffect") {
		t.Fatalf("removed structural token = %q", removed)
	}
}
