package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/mattwalker/revui/internal/gitrepo"
	"github.com/mattwalker/revui/internal/ui"
)

var version = "dev"

func main() {
	base := flag.String("base", "", "base branch or revision to review against (default: repository default branch)")
	showVersion := flag.Bool("version", false, "print the revui version")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "revui — review a branch before it reaches GitHub\n\nUsage:\n  revui [--base <branch>]\n\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintln(flag.CommandLine.Output(), "\nEnvironment:\n  REVUI_AGENT_COMMAND  command that reads the review prompt from stdin\n                       (default: codex exec --sandbox workspace-write -)")
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("revui", version)
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	repo, err := gitrepo.Open(cwd, *base)
	if err != nil {
		fatal(err)
	}
	model, err := ui.New(repo)
	if err != nil {
		fatal(err)
	}
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fatal(err)
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "revui:", err); os.Exit(1) }
