package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/decision-log/decision-log/internal/gitx"
	"github.com/decision-log/decision-log/internal/note"
	"github.com/decision-log/decision-log/internal/secrets"
	"github.com/decision-log/decision-log/internal/store"
)

// fileList accepts --files more than once and comma-separated values.
type fileList []string

func (f *fileList) String() string { return strings.Join(*f, ",") }

func (f *fileList) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*f = append(*f, p)
		}
	}
	return nil
}

// cmdAdd saves one note. Its output is one short line because Claude reads it.
func cmdAdd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	decision := fs.String("decision", "", "what was decided (one line)")
	why := fs.String("why", "", "the concrete reason (one line)")
	rejected := fs.String("rejected", "", "the alternative(s) not chosen, and why (one line)")
	by := fs.String("by", "", `who made the call: "user" or "claude"`)
	var files fileList
	fs.Var(&files, "files", "files the decision is about, relative to the current directory (repeatable or comma-separated)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "decision-log add: unexpected argument %q (quote each value)\n", fs.Arg(0))
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "decision-log add: %v\n", err)
		return 1
	}

	t := now()
	n := note.New(*decision, *why, *rejected, *by, files, t)

	// A note made outside a git repo stays standalone, never attached to a commit.
	if repo, err := gitx.Find(cwd); err == nil {
		n.Repo = note.Repo{RootCommit: repo.RootCommit(), Remote: repo.Remote(), Path: repo.Root}
		n.Branch = repo.Branch()
		rel := make([]string, len(n.Files))
		for i, f := range n.Files {
			rel[i] = repo.RelPath(cwd, f)
		}
		n.Files = note.CleanFiles(rel)
	}

	redacted := false
	for _, field := range []*string{&n.Decision, &n.Why, &n.Rejected} {
		var changed bool
		*field, changed = secrets.Redact(*field)
		redacted = redacted || changed
	}

	if err := n.Validate(); err != nil {
		fmt.Fprintf(stderr, "decision-log add: note not saved:\n%v\n", err)
		return 2
	}

	home, err := store.DefaultHome()
	if err != nil {
		fmt.Fprintf(stderr, "decision-log add: %v\n", err)
		return 1
	}
	s, err := store.Open(home)
	if err != nil {
		fmt.Fprintf(stderr, "decision-log add: %v\n", err)
		return 1
	}

	verb := "saved"
	// A corrupt file elsewhere shouldn't stop this save, so the error from All is ignored.
	existing, _ := s.All()
	for _, old := range existing {
		if old.Status == note.StatusActive && old.SameDecision(n) {
			old.Merge(n, t)
			n, verb = old, "updated"
			break
		}
	}

	if err := s.Save(n); err != nil {
		fmt.Fprintf(stderr, "decision-log add: %v\n", err)
		return 1
	}

	msg := fmt.Sprintf("decision-log: %s %s", verb, n.ID)
	if redacted {
		msg += " (secrets redacted)"
	}
	fmt.Fprintln(stdout, msg)
	return 0
}
