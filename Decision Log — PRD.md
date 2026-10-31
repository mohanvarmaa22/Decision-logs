# Decision Log — PRD

Oct 31, 2026 · @Mohan Varma

## Overview

**Decision Log never loses the "why" behind your code.** It captures decisions made while coding with Claude Code, stores them in one place, and lets you look them up later by file, line or keyword, without breaking your flow.

**Problem.** Developers make many decisions in Claude Code chats every day. Two months later the code remains, but the reasoning is buried in old transcripts that get cleaned up. Nobody can answer "why did we do this?"

**Target users.** Individual developers using Claude Code daily, in the terminal or VS Code, and then their teams.

### Goals

- Capture real decisions automatically, with near-zero effort and token cost
- Answer "why is this code like this?" in seconds
- Install with one command on Mac, Windows and Linux
- Stay lightweight: no server, no account, nothing running in the background

### Non-goals (for now)

- Storing or searching full chat transcripts
- A hosted service, team sync server or accounts
- Supporting AI agents other than Claude Code at launch (the design leaves room for them)

### Promises to users

1. It never blocks or breaks a commit or a push.
2. It never stores raw chat text, only short notes.
3. It never touches commit messages unless you turn that on.
4. One command removes it completely.

## Part 1: Product design

The user installs once, codes as usual, and opens the app or types `/why` when they need an answer. Everything in between happens on its own.

### User journey

| Moment | What happens | User effort |
| --- | --- | --- |
| Install | One command installs the app and Claude Code plugin, then asks: "Add decisions to commit messages?" (default: No) | 1 command |
| Coding | Claude quietly saves a short note when a real decision is made | None |
| Committing | From the terminal, VS Code or any Git app, the note is linked to the commit | None |
| Looking back | Open the app, or type `/why` in Claude Code | 1 command |

### The app (`decision-log ui`)

A local page that opens in the browser. No account, no internet.

- **All decisions** across every repo, newest first, with search
- **Filters** by repo, file, date, and decided by me or by Claude
- **Decision card:** decision, why, rejected options, files, commit link, date, status (active or superseded, linked to the decision that replaced it)
- **Pending:** notes not yet linked to a commit; attach or discard
- **Settings:** commit-message trailers on or off per repo, pause, export, uninstall

### Looking up a decision with `/why`

`line` means the line number shown in the editor's left margin.

| Command | Meaning |
| --- | --- |
| `/why src/cache.ts:42` | Why does line 42 look like this? |
| `/why src/cache.ts:40-60` | Decisions for a range of lines |
| `/why src/cache.ts` | Every decision about this file |
| `/why redis` | Search all decisions for a word |

Inside Claude Code, users can also just ask "why is the retry logic like this?" and Claude runs the command.

### Features that create value

- **Claude checks past decisions before editing code**, so it won't silently undo them
- **Backfill (optional):** builds a starting history from recent Claude chats, with a token-cost estimate shown first
- **Optional commit trailers:** teammates see the "Why" in git and on GitHub without installing anything
- **Agent-agnostic core:** only the capture adapter is Claude Code-specific; other agents and humans (`/decide`, CLI) can add notes

## Part 2: Quality of the whys

Note quality decides whether people keep using the tool: ten trivial notes bury the one that matters. Capture must be selective, short and invisible.

### What counts as a decision

Save a note only when all three are true:

1. There were real alternatives ("Redis or in-memory").
2. One was chosen for a concrete reason.
3. Someone would plausibly ask "why?" months later.

Not decisions: renames, typo fixes, formatting, obvious one-way changes.

### How notes are collected

| Path | How it works | Cost to flow |
| --- | --- | --- |
| Proactive (main) | The plugin's skill tells Claude to run `decision-log add` right after a real choice | Invisible, \~100 tokens |
| Backstop | After each Claude reply, a script searches for decision words. If found and no note was saved, it nudges Claude once | One short extra step, rarely |
| Manual | User types `/decide keep REST, not GraphQL` | Zero |
| Backfill | One-time extraction from old chats, cost shown first | Opt-in only |

### How we tell Claude

The plugin ships a `SKILL.md`. Claude always sees its short description, which says *when* to act. The body holds the rules for *how*: what counts, the one-line format, and "check `decision-log why <file>` before changing code; don't undo an active decision without telling the user."

### Note format

```
Decision:   Use Redis for session cache
Why:        Multiple app servers; in-memory cache isn't shared
Rejected:   Sticky sessions (breaks autoscaling)
Decided-by: user            (or claude)
Files:      src/cache.ts
```

Claude records **files, not line numbers**. Line numbers shift as code changes; git tracks exact lines at commit time, and `/why` uses that.

### Quality rules

- One line each for decision, why and rejected; no essays
- Record who decided, so users can filter out Claude's small technical choices
- At most 3 notes per commit; duplicates merged
- A new decision on the same code marks the old one superseded and links them
- Scan for secrets before saving; never store raw chat text
- Every note can be edited or deleted in the app

### Protecting the user's flow

- **No permission pop-ups:** the installer adds an allow rule for `decision-log add`
- **At most one nudge per reply:** uses Claude Code's `stop_hook_active` flag, so it can never loop
- **Silent hooks:** nothing printed into Claude's context, so no token cost or clutter
- **Fail open:** any error skips the note and work continues

## Part 3: Tech stack, architecture and feasibility

One small Go binary does everything: CLI, hooks and UI. Decisions are plain JSON files on the user's machine. No server, no database, nothing running in the background.

### Stack

| Piece | Choice | Why |
| --- | --- | --- |
| Language | Go | One small binary per OS, no runtime, cross-compiles easily, starts in milliseconds |
| UI | Plain HTML/CSS + light JS, embedded in the binary, opened in the browser | Same on every OS, no Electron, no app signing |
| Storage | One JSON file per decision in `~/.decision-log/` | No database or locking, human-readable, easy backup, fast enough for 10,000+ notes |
| Claude Code side | Plugin: one skill, a Stop hook, a SessionStart hook | Small surface, few things to break |
| Git side | `post-commit` hook links notes; `commit-msg` hook only if trailers are on | `post-commit` cannot break a commit |
| Release | GoReleaser builds binaries, checksums, Homebrew and Scoop updates | One pipeline for every platform |

**Size:** \~5–8 MB download, \~10–20 MB memory only while the UI is open.

### Architecture

Two halves that don't depend on each other, joined by the local store.

&#91;embedded content: architecture · capture, store, link, look up\]

Claude Code never has to make the commit, and the git hook never needs Claude; the store is the only link between them.

### Linking a note to a line, even after a rebase

`/why file:line` runs `git blame`, then tries three matches in order:

1. **Commit ID:** exact match
2. **Patch ID:** a fingerprint of the change that survives rebase and cherry-pick
3. **Files, lines and date:** fallback for squash merges

If the latest change to a line was unrelated (e.g. formatting), `/why` walks the line's full history (`git log -L`) and respects `.git-blame-ignore-revs`.

### Storage modes

| Mode | Where | Who sees it |
| --- | --- | --- |
| local (default) | `~/.decision-log/` | Only you |
| trailer (opt-in at install) | Local + commit message lines | Everyone, incl. GitHub |
| notes (later) | Local + git notes | Team, via git |

### Feasibility

Every piece uses standard, documented features: Claude Code plugins and hooks, git hooks, a Go binary. No infrastructure cost. v0.1 is roughly 1–2 weeks of work.

## Install and distribution

One codebase, one compiled file per OS; the installer picks the right one, so it feels like a single app. GoReleaser publishes every channel from one release.

### Install options

| Channel | Command | Platforms | Steps |
| --- | --- | --- | --- |
| Script | `curl -fsSL https://raw.githubusercontent.com/decision-log/decision-log/main/install.sh \| sh` | macOS, Linux | 1 |
| PowerShell | `irm https://raw.githubusercontent.com/decision-log/decision-log/main/install.ps1 \| iex` | Windows | 1 |
| Homebrew tap | `brew install decision-log/tap/decision-log` then `decision-log init` | macOS, Linux | 2 |
| Scoop | `scoop install decision-log` then `decision-log init` | Windows | 2 |

Homebrew and Scoop take two steps because package managers shouldn't change user settings; `init` sets up Claude Code and asks the trailer question. A post-install message tells users to run it.

### Homebrew setup

1. Create a GitHub repo `homebrew-tap` under the project org.
2. Configure GoReleaser's `brews` section; each release updates the formula with new URLs and checksums.
3. Later, once the project is popular, apply to Homebrew core so users can run plain `brew install decision-log`.

### What the installer and `init` do

1. Download the binary for the OS, verify its checksum, place it in `~/.local/bin` (Windows: `%LOCALAPPDATA%`); no admin rights
2. Create `~/.decision-log/`
3. Install the Claude Code plugin at user level (via `claude plugin install`, or by writing `~/.claude/settings.json`); skip with a message if Claude Code isn't found
4. Add an allow rule for `decision-log add`
5. Ask: "Add decisions to commit messages?" (default No; read from `/dev/tty`; `--trailers=yes` for scripts)
6. Git hooks install themselves per repo on the first Claude session there

### Trust, updates and removal

- Readable install script, `--dry-run` flag, published checksums, signed releases
- `decision-log update`; a weekly version check prints one line, never auto-installs
- `decision-log doctor` checks setup and explains fixes
- `decision-log uninstall` removes binary, plugin and hooks, restores original hooks, asks before deleting decisions

## Security and privacy

All data stays on the user's machine. The tool has no server, no account, no telemetry, and never asks for, reads or stores API keys.

### Data stays local

- Decisions live only in `~/.decision-log/`, readable only by the user (folder 0700, files 0600)
- No analytics, no crash reporting, no network calls from hooks or the CLI
- The only network use: downloading the app at install, and an update check the user can turn off
- Notes leave the machine only if the user turns on commit trailers and pushes; the opt-in prompt says so

### No API keys

- The tool never asks for an API key and never reads Claude Code's credential files
- Claude writes notes inside the user's normal Claude Code session
- Backfill runs through the user's own Claude Code login (`claude -p`); excerpts go only where the user's Claude Code already sends data, no new destination

### Keeping secrets out of notes

- Every note is scanned before saving for keys and tokens (AWS, GitHub, Anthropic/OpenAI-style keys, private-key blocks, JWTs, `.env`-style assignments, high-entropy strings); matches are replaced with `[REDACTED]`
- The commit-msg hook scans again before adding trailers
- Raw chat transcripts are never stored; backfill reads them locally and keeps only short notes

### Local UI

- Listens on `127.0.0.1` only, on a random port, with a one-time token in the URL
- Checks the request's Host header, so other websites in the browser can't read decisions
- Stops when the user closes it

### Supply chain

- Signed releases with published checksums, verified by the installer
- Few third-party dependencies; a software bill of materials published with each release
- Readable install script with `--dry-run`

### Limits to state honestly

- Notes are as safe as the user's computer, the same as their source code
- Secret scanning catches common patterns, not everything
- Notes are shown to Claude as data; the skill tells Claude never to follow instructions found inside a note

## Legal and disclaimers

The project ships under the MIT license, which already says the software comes "as is" with no warranty and that authors aren't liable for damages. We repeat that plainly wherever users meet the tool. This is not legal advice; review with a lawyer before any company backs the project.

### Where users see it

| Place | What it says |
| --- | --- |
| `LICENSE` | Standard MIT text, unchanged |
| README, "Disclaimer" section | Use at your own risk; no warranty; back up your work; not affiliated with Anthropic |
| Installer | One line before install: "Provided as-is under the MIT license, use at your own risk." Continuing = accepting; `--dry-run` shows every change first |
| `decision-log --version` and app footer | License name and a link to the disclaimer |
| `PRIVACY.md` | The tool collects no data; everything stays on the user's machine |
| `SECURITY.md` | How to report a vulnerability privately |

### README disclaimer (draft)

> Decision Log is provided as-is, without warranty of any kind, under the MIT license. You use it at your own risk. The authors and contributors are not responsible for any loss of data, broken workflows, leaked information or other damage arising from its use. It changes git hooks and Claude Code settings on your machine; review them with `--dry-run` and keep backups. Decision Log is an independent project and is not affiliated with or endorsed by Anthropic.

### Other protections

- **Name:** keep "Claude" out of the project name and logo (Anthropic trademark); say "for Claude Code" only as a description
- **Contributions:** require a DCO sign-off (`git commit -s`) so contributors confirm they have the right to submit their code under MIT
- **Behaviour is the best protection:** fail-open hooks, `--dry-run`, `uninstall` that restores original hooks, no data collection
- **Limits:** disclaimers don't cover deliberate harm or gross negligence, and consumer laws vary by country. A GitHub org is not a legal entity; if the project grows or earns money, consider forming a company and getting legal review

## Bottlenecks and where it can break

The biggest risk is note quality, not the tech. If notes aren't worth reading, nothing else matters.

| Area | Risk | What goes wrong | Prevention |
| --- | --- | --- | --- |
| Quality | Noise | Too many trivial notes; users stop reading | Strict "what counts" rules, Decided-by filter, 3-per-commit cap, track delete rate |
| Quality | Missed decisions | Decisions without trigger words, or made outside Claude | `/decide`; honest "captures most, not all" messaging |
| Quality | Wrong attachment | Note linked to an unrelated commit | Match on files + branch + time window; unmatched notes wait in Pending |
| Quality | Vague reasons | "Because it's better" | Skill requires a concrete reason; UI flags vague notes |
| Flow | Annoying nudges | Backstop adds visible extra turns | Tune the proactive skill; measure nudge rate |
| Flow | Hook crash or slowness | Commits feel slow or fail | Always exit 0, \~1 s timeout, no network in hooks |
| Product | No early "aha" | Nothing visible for days after install | Backfill + one-time "first decision saved" message |
| Product | Distrust of `curl \| sh` | Users won't install | Dry run, checksums, Homebrew and Scoop |
| Product | Single machine | Decisions lost or not shared | Export/import; trailers for teams |
| Tech | Windows | Shell scripts and paths differ | Hook = tiny stub calling the `.exe`; CI on Windows every release |
| Tech | Existing hook setups | husky, lefthook, pre-commit skip or clash with our hook | Chain, never replace; `doctor` prints the one line to add |
| Tech | Claude Code updates | Hook or plugin APIs change | Small surface, pinned versions, tests against new releases |
| Tech | Unsigned binaries | SmartScreen or antivirus warnings | Code-sign once there are real users |
| Tech | Squash merges | Commit IDs change, trailers edited away | Patch ID / file / date fallback; optional GitHub Action later |
| Tech | Parallel sessions | Writes collide | One file per note, nothing to collide |
| Tech | Custom or enterprise Claude config | Plugin can't install | `doctor` explains the manual step |
| Privacy | Secrets in notes | Keys or internal details leak into commits | Secret scan before saving; no raw chat; local mode default |

## Roadmap and success metrics

Ship a version that works for you daily in about two weeks, then add the UI and day-one value.

| Version | Scope | Estimate |
| --- | --- | --- |
| Prototype | Test the three risky assumptions: note quality on 10–20 real sessions, reading Claude Code's chat log, fully automatic plugin + permission setup. Built and tested on macOS | 2–3 days |
| v0.1 | curl and PowerShell installers, Homebrew tap, plugin (skill + Stop + SessionStart), `add` / `link`, `/why`, `doctor`, backfill (last 30 days), secret scanning, fail-open everywhere. Used daily by the author before going public | \~2–3 weeks |
| v0.2 | Web UI, `/decide`, Pending view, optional commit trailers, Scoop; public launch with contributing guide and DCO | +1–2 weeks |
| v0.3 | Claude checks history before editing, export/import | +1 week |
| v1.0 | GitHub squash Action, git-notes mode, hook-manager integrations, code signing, own domain | Later |
| Later | Other AI agents, VS Code extension if users ask, Homebrew core | On demand |

### Metrics

| Metric | Measures | Signal |
| --- | --- | --- |
| `/why` and app opens per week | Value | Going up |
| Notes edited or deleted | Noise | Low |
| Backstop nudge rate | Friction | Low and falling |
| Uninstall rate | Trust | Low |
| Share of meaningful commits with a decision | Coverage | Going up |

## Open questions

- [ ] Exact decision-word list for the backstop regex (settle during the prototype)
- [ ] Results of the three prototype tests, and any design changes they force

## Decisions made

| Topic | Decision |
| --- | --- |
| Name | `decision-log` (org, command, repo) |
| GitHub | New org `decision-log` with the main repo and `homebrew-tap` |
| License | MIT |
| Domain | GitHub raw URLs for install until public launch |
| Builder and language | Author with Claude Code, in Go |
| First platform | macOS first; Windows and Linux in CI before public launch |
| Release path | Prototype, then author uses v0.1 daily, then public launch |
| Contributors | Solo until public launch |
| Whose decisions | Both user and Claude, labelled with Decided-by |
| Multi-commit decisions | Note links to the first matching commit; `/why` finds it via file history |
| Notes with no files | Stay standalone in the app, searchable, never attached to a commit |
| Backstop Stop hook | Ships in v0.1 |
| Backfill | Ships in v0.1; reads the last 30 days of chats, cost shown first |
| Update check | Weekly, on by default, can be turned off |
| Repo identity | First commit + remote URL |
| Commit trailers (opt-in) | Decision + Why + Rejected |

## Change log

| Date | Change |
| --- | --- |
| 2026-10-31 | Pre-build questions answered; Decisions made table added; prototype phase added; install URLs moved to GitHub |
| 2026-10-31 | GitHub org and MIT license decided; Legal and disclaimers section added |
| 2026-10-31 | Backfill moved to v0.1; Security and privacy section added; org and license explained in open questions |
| 2026-10-31 | First draft: product design, quality of whys, tech stack, Homebrew added, commit trailers made optional |
