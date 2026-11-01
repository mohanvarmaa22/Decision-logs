# decision-log

Never lose the "why" behind your code. Decision Log captures the decisions made while coding with Claude Code as short notes, keeps them on your machine, and lets you look them up later.

> **Status: prototype.** `add`, `list` and `why` exist so far. See [`Decision Log — PRD.md`](./Decision%20Log%20—%20PRD.md) for the full design.

## Try the prototype (macOS)

```bash
make install                    # builds and installs to ~/.local/bin/decision-log
claude --plugin-dir ./plugin    # start Claude Code with the plugin loaded
```

To skip the permission prompt each time Claude saves a note, add this to `permissions.allow` in `~/.claude/settings.json`:

```json
"Bash(decision-log add:*)"
```

Read what's been captured:

```bash
decision-log list           # all repos, newest first
decision-log list --here    # just the current repo
decision-log why src/cache.ts:42    # also: file:40-60, file, or a keyword
```

Notes are plain JSON files in `~/.decision-log/decisions/`, readable only by you. To remove everything: `make uninstall && rm -rf ~/.decision-log`.

## Development

```bash
make check    # vet, tests, gofmt and plugin validation
go test ./internal/store -run TestSaveAndAll    # a single test
```

## Disclaimer

Decision Log is provided as-is, without warranty of any kind, under the MIT license. You use it at your own risk. The authors and contributors are not responsible for any loss of data, broken workflows, leaked information or other damage arising from its use. It changes git hooks and Claude Code settings on your machine; review them with `--dry-run` and keep backups. Decision Log is an independent project and is not affiliated with or endorsed by Anthropic.
