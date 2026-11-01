package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
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
	supersedes := fs.String("supersedes", "", "ID of an earlier note this decision replaces")
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
		n.BaseCommit, _ = repo.Head() // a repo with no commits has none
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

	var replaced *note.Note
	if *supersedes != "" {
		i := slices.IndexFunc(existing, func(o *note.Note) bool { return o.ID == *supersedes })
		if i < 0 || *supersedes == n.ID {
			fmt.Fprintf(stderr, "decision-log add: note not saved: no earlier note with id %q\n", *supersedes)
			return 2
		}
		replaced = existing[i]
	}

	if err := s.Save(n); err != nil {
		fmt.Fprintf(stderr, "decision-log add: %v\n", err)
		return 1
	}
	if replaced != nil {
		replaced.Status, replaced.SupersededBy, replaced.UpdatedAt = note.StatusSuperseded, n.ID, t.UTC()
		if err := s.Save(replaced); err != nil {
			fmt.Fprintf(stderr, "decision-log add: saved %s, but couldn't mark the old note superseded: %v\n", n.ID, err)
			return 1
		}
	}

	msg := fmt.Sprintf("decision-log: %s %s", verb, n.ID)
	if redacted {
		msg += " (secrets redacted)"
	}
	fmt.Fprintln(stdout, msg)
	return 0
}
