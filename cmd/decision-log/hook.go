package main

import (
	"fmt"
	"io"
	"os"
)

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
	if args[0] == "session-start" {
		fmt.Fprintln(stdout, sessionStartNote)
	}
	return 0
}
