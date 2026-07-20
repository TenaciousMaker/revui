package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/TenaciousMaker/revui/internal/gitrepo"
	"github.com/TenaciousMaker/revui/internal/ui"
)

var version = "dev"

var workingDirectory = os.Getwd
var openRepository = gitrepo.Open
var runProgram = func(model ui.Model, input io.Reader, output io.Writer) error {
	defer model.Close()
	_, err := tea.NewProgram(model, tea.WithInput(input), tea.WithOutput(output)).Run()
	return err
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("revui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	base := flags.String("base", "", "base branch or revision to review against")
	showVersion := flags.Bool("version", false, "print the revui version")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "revui — review your PR before it's a PR")
		_, _ = fmt.Fprintln(stderr, "\nUsage:\n  revui [--base <branch>]\n\nOptions:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "revui: unexpected argument %q\n", flags.Arg(0))
		flags.Usage()
		return 2
	}
	if *showVersion {
		_, _ = fmt.Fprintln(stdout, "revui", buildVersion())
		return 0
	}
	cwd, err := workingDirectory()
	if err != nil {
		return fail(stderr, err)
	}
	repo, err := openRepository(cwd, *base)
	if err != nil {
		return fail(stderr, err)
	}
	model, err := ui.New(repo)
	if err != nil {
		return fail(stderr, err)
	}
	model.EnableWatching()
	if err := runProgram(model, stdin, stdout); err != nil {
		return fail(stderr, err)
	}
	return 0
}

func buildVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func fail(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintln(stderr, "revui:", err)
	return 1
}
