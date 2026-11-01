package main

import (
	"fmt"
	"io"
	"os"
	"time"

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
	case "post-commit":
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer func() { _ = recover() }()
			linkHead()
		}()
		select {
		case <-done:
		case <-time.After(hookBudget):
		}
	}
	return 0
}

// linkHead ignores every error: a failed link leaves the note pending.
func linkHead() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	repo, err := gitx.Find(cwd)
	if err != nil {
		return
	}
	home, err := store.DefaultHome()
	if err != nil {
		return
	}
	s, err := store.Open(home)
	if err != nil {
		return
	}
	_ = link.Head(s, repo, now())
}
