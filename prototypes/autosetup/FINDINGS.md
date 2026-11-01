# Prototype test 3: fully automatic setup

**Result: go.** One command can install everything without manual steps. The skill alone rarely fired, so the plugin now adds a short SessionStart instruction, and notes were then saved in every decision run. Real sessions still need measuring in test 1.

Measured on 2026-10-31 with Claude Code on macOS, using temporary config and data directories. The real `~/.claude/settings.json` was only read. Samples are small.

## Plugin install

| Check | Result |
| --- | --- |
| `claude plugin marketplace add <local path>` and `claude plugin install decision-log@decision-log --scope user` | Work with no prompt, stdin closed; install took under 1 s |
| Same from GitHub (`mohanvarmaa22/Decision-logs`) | Works |
| Install twice | Reports already installed; safe to re-run |
| `claude plugin details` | About 87 tokens always on, about 520 when the skill is invoked |
| `CLAUDE_CONFIG_DIR` | Isolates all of this, so the installer can be tested without touching real settings |

**`uninstall` gap:** `claude plugin uninstall` and `marketplace remove` leave `"enabledPlugins": {}`, `"extraKnownMarketplaces": {}` and a cache directory marked `.orphaned_at`. The PRD's "removes it completely" needs `decision-log uninstall` to account for these, or the promise needs softer wording.

## Allow rule

`claude -p` runs with Haiku, with permissions from a temporary `--settings` file:

| Run | Rule | Outcome |
| --- | --- | --- |
| Plain `decision-log add ...` | No | Denied, no note (control) |
| Plain `decision-log add ...` | `Bash(decision-log add:*)` | Saved, no denial |
| Multi-line with `\` continuations, as in `SKILL.md` | Same | Saved, no denial |
| `decision-log add ... && touch PWNED1` | Same | Denied, nothing ran |
| `decision-log add --decision "$(touch PWNED2)" ...` | Same | Denied, nothing ran |

The rule grants only what it should in these runs: chaining and command substitution still need approval.

## Writing the rule safely

`prototypes/autosetup` adds and removes the rule. Tests and a temporary copy of real settings show that it:

- keeps other keys, values and order; removal restores the original content
- is safe to re-run
- refuses JSON with comments, invalid JSON and unexpected types, leaving the file untouched
- keeps one backup of the original, writes atomically, keeps file mode, and writes through symlinks

## PATH

`~/.local/bin` was not on the author's zsh PATH, so `decision-log` was "command not found" after a correct install. The installer and `doctor` must check this and print the exact line to add. A check from another process can see a different PATH, so verify using the user's login shell.

## Skill triggering

The skill alone loaded in every run but often did not fire. The fix is a SessionStart hook that adds a 56-word instruction with the full command, so Claude can act without loading the skill. It costs tokens in every session, and it is an exception to the PRD's "silent hooks" rule.

Runs saving a note, using one-prompt `claude -p` sessions:

| Prompt | Model | Skill only | Skill + SessionStart |
| --- | --- | --- | --- |
| User states a decision | Haiku | 0 of 4 | 3 of 3 |
| User states a decision | Sonnet | 1 of 3 | 3 of 3 |
| User states a decision | Opus | 2 of 2 | 3 of 3 |
| Claude picks (SQLite or Postgres) | Haiku, Sonnet, Opus | Not run | 3 of 3 |
| No decision (meaning of HTTP 404) | Haiku, Sonnet, Opus | Not run | 0 of 3 |

With the instruction, Claude skipped the skill and ran the command directly. An Opus run cost about $0.14, against $0.16 with a warm cache when it loaded the skill first.

**Limits:** these are short runs using the same few prompts, and the prompt named the decision plainly. Opus already fired 2 of 2 without the hook. Harder non-decisions (renames, typos during real edits) and decisions buried in long sessions are untested, so noise and recall remain open for test 1.

**Duplicates:** repeat runs saved the same decision in different words, which `SameDecision` does not merge because it compares exact text. Within one session this should be rare, but check for it in test 1.

## Open

- Does the instruction add noise, or miss decisions, in real sessions?
- Reworded duplicates across sessions.
