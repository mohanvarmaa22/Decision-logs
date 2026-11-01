package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/decision-log/decision-log/internal/githook"
	"github.com/decision-log/decision-log/internal/gitx"
	"github.com/decision-log/decision-log/internal/link"
	"github.com/decision-log/decision-log/internal/store"
)

// hookBudget bounds a git hook so it can never slow a commit noticeably. Note
// writes are atomic, so giving up midway can't corrupt anything.
const hookBudget = time.Second

// DisableEnv turns all hooks off, for scripts and for backfill's own claude -p calls.
const DisableEnv = "DECISION_LOG_DISABLE"

// sessionStartNote is added to every session's context, so every word costs
// tokens. It carries the full command so Claude can act without loading the skill.
const sessionStartNote = `decision-log: when a real choice is settled in this session (real alternatives, one picked for a concrete reason, someone may ask "why?" months later), run this before you reply, without announcing it:
decision-log add --decision "..." --why "..." --rejected "..." --by user|claude --files a,b
One short line per value. Skip renames, typos, formatting and obvious changes.`

// cmdHook handles Claude Code hooks. It always exits 0: a hook must never get
// in the way of the user's work.
func cmdHook(args []string, stdout io.Writer) int {
	if os.Getenv(DisableEnv) != "" || len(args) != 1 {
		return 0
	}
	switch args[0] {
	case "session-start":
		fmt.Fprintln(stdout, sessionStartNote)
		withinBudget(installGitHook)
	case "post-commit":
		withinBudget(linkHead)
	}
	return 0
}

// withinBudget runs fn but never waits past hookBudget or lets a panic escape.
func withinBudget(fn func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }()
		fn()
	}()
	select {
	case <-done:
	case <-time.After(hookBudget):
	}
}

// hookContext finds the repo and data directory. It's false outside a repo.
func hookContext() (*gitx.Repo, string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", false
	}
	repo, err := gitx.Find(cwd)
	if err != nil {
		return nil, "", false
	}
	home, err := store.DefaultHome()
	if err != nil {
		return nil, "", false
	}
	return repo, home, true
}

// linkHead ignores every error: a failed link leaves the note pending.
func linkHead() {
	repo, home, ok := hookContext()
	if !ok {
		return
	}
	s, err := store.Open(home)
	if err != nil {
		return
	}
	_ = link.Head(s, repo, now())
}

// installGitHook ignores every error: without the hook, notes just stay pending.
func installGitHook() {
	repo, home, ok := hookContext()
	if !ok {
		return
	}
	bin, err := os.Executable()
	if err != nil {
		return
	}
	if _, err := store.Open(home); err != nil { // creates home with private permissions
		return
	}
	_, _ = githook.Install(repo, bin, home)
}
