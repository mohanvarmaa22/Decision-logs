# Prototype test 2: reading Claude Code's chat logs

**Result: go.** The logs can be read reliably enough for the backstop and for backfill, with the design changes below. One question stays open: the nudge in scripted runs.

Measured on 2026-10-31 with `go run ./prototypes/transcript`, on one author's machine: Claude Code 2.1.285 to 2.1.314, 15 logs, 6,319 lines. Live hook checks were 2 `claude -p` runs on macOS. This is a small sample. No chat text belongs in this file; `--samples` output must never be committed.

## Log reading

| Check | Result | What it means |
| --- | --- | --- |
| Malformed lines | 0 of 6,319 | Still skip bad lines and unknown types; the format is undocumented |
| Longest line | 1.1 MB | `bufio.Scanner`'s default 64 KB limit would fail; use `bufio.Reader` |
| Biggest log | 17.2 MB parsed in 55 ms | Fits the hook's ~1 s budget without tail-reading |
| Largest turn | 4.2 MB (p50 43 KB) | A small fixed tail window would miss turns |
| Same UUID in more than one log | 294 of 2,521 | Resumed sessions copy history; backfill must dedupe across logs (168 turns became 148) |
| Prompts sharing one parent | 3 | Rewinds exist; backfill must not treat abandoned branches as decisions |
| Subagent turns | Separate files, 0 sidechain lines in main logs | Top-level `*.jsonl` is enough |

`type: "user"` also covers tool results, meta lines, compact summaries and slash-command text. Only what a person typed may start a turn.

## Stop hook (live)

- Input includes `transcript_path`, `stop_hook_active`, `last_assistant_message` and `cwd`.
- The final reply and the turn's tool calls were already in the log when the hook ran (2 of 2 runs). Two runs do not rule out a race, so use `last_assistant_message` for the text and treat the log as best effort.
- `{"decision":"block","reason":"..."}` nudged exactly once. The next Stop had `stop_hook_active: true`, so it cannot loop.
- **Problem:** the nudge's reply becomes the session's last message. In both runs `claude -p` printed `NOTED`, not the requested reply. A nudge would change the output of the user's scripts.
- `entrypoint` does not identify scripted runs: `claude -p` started from a VS Code session logged `claude-vscode`.

## Decision words

Turns containing a match, after deduping and ignoring code:

| Set | Hit rate | Quality |
| --- | --- | --- |
| Broad (`recommend`, `instead of`, `prefer`, ...) | 33% of 168 turns, before dedupe | Mostly noise; rejected |
| Strict (`decided`, `chose`, `going with`, `use X instead of Y`, ...) | 7 of 148 turns (5%) | About 3 of 10 sampled hits were real decisions |

Use the strict set for now. It is only a trigger: the nudge asks Claude to judge whether a real decision was made. Real recall is unknown until test 1 has notes to compare against.

## Design changes

1. **Backstop:** take text from `last_assistant_message`, strip code, apply the strict set, and read the log only to check for `decision-log add` in the current turn.
2. **Skill:** after a nudge, save the note if one is due and otherwise end with no extra commentary.
3. **Backfill:** dedupe UUIDs across logs, follow only the live branch, and set `DECISION_LOG_DISABLE=1` on its own `claude -p` calls so the backstop stays off.
4. **Hooks:** honour `DECISION_LOG_DISABLE=1` as an opt-out for scripts.

## Open

- Can a Stop hook detect a `claude -p` run? No field found yet. Settle this in `feat/claude-hooks`.
