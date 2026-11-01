package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/decision-log/decision-log/internal/gitx"
	"github.com/decision-log/decision-log/internal/note"
	"github.com/decision-log/decision-log/internal/store"
)

func cmdList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	here := fs.Bool("here", false, "only notes from the current repo")
	limit := fs.Int("limit", 0, "show at most N notes (0 = all)")
	asJSON := fs.Bool("json", false, "print notes as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	home, err := store.DefaultHome()
	if err != nil {
		fmt.Fprintf(stderr, "decision-log list: %v\n", err)
		return 1
	}
	s, err := store.Open(home)
	if err != nil {
		fmt.Fprintf(stderr, "decision-log list: %v\n", err)
		return 1
	}
	notes, err := s.All()
	if err != nil {
		// Unreadable files are reported, but the readable notes still print.
		fmt.Fprintf(stderr, "decision-log list: skipped unreadable notes:\n%v\n", err)
	}

	if *here {
		cwd, _ := os.Getwd()
		repo, err := gitx.Find(cwd)
		if err != nil {
			fmt.Fprintln(stderr, "decision-log list: --here needs to run inside a git repo")
			return 2
		}
		notes = inRepo(notes, repo)
	}
	if *limit > 0 && len(notes) > *limit {
		notes = notes[:*limit]
	}

	if *asJSON {
		if notes == nil {
			notes = []*note.Note{}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(notes); err != nil {
			fmt.Fprintf(stderr, "decision-log list: %v\n", err)
			return 1
		}
		return 0
	}

	if len(notes) == 0 {
		fmt.Fprintln(stdout, "No decisions yet.")
		return 0
	}
	for i, n := range notes {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		printNote(stdout, n)
	}
	return 0
}

func inRepo(notes []*note.Note, repo *gitx.Repo) []*note.Note {
	root := repo.RootCommit()
	var out []*note.Note
	for _, n := range notes {
		if n.InRepo(root, repo.Root) {
			out = append(out, n)
		}
	}
	return out
}

func printNote(w io.Writer, n *note.Note) {
	status := ""
	if n.Status == note.StatusSuperseded {
		status = "  [superseded]"
	}
	fmt.Fprintf(w, "%s  %-6s  %s%s\n", n.CreatedAt.Local().Format("2006-01-02 15:04"), n.DecidedBy, n.Decision, status)
	fmt.Fprintf(w, "  Why:       %s\n", n.Why)
	fmt.Fprintf(w, "  Rejected:  %s\n", n.Rejected)
	if len(n.Files) > 0 {
		fmt.Fprintf(w, "  Files:     %s\n", strings.Join(n.Files, ", "))
	}
	where := repoLabel(n)
	if n.Branch != "" {
		where += " @ " + n.Branch
	}
	fmt.Fprintf(w, "  Repo:      %s\n", where)
	commit := "pending"
	if n.Commit != "" {
		commit = n.Commit[:min(len(n.Commit), 12)]
	}
	fmt.Fprintf(w, "  Commit:    %s\n", commit)
}

func repoLabel(n *note.Note) string {
	switch {
	case n.Repo.Remote != "":
		return n.Repo.Remote
	case n.Repo.Path != "":
		return n.Repo.Path
	default:
		return "(no repo)"
	}
}
