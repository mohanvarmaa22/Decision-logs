// Command decision-log captures and looks up the decisions behind code.
package main

import (
	"fmt"
	"io"
	"os"
	"time"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// now is replaced in tests.
var now = time.Now

const usage = `decision-log: never lose the "why" behind your code.

Usage:
  decision-log add --decision "..." --why "..." --rejected "..." --by user|claude [--files a,b]
  decision-log list [--here] [--limit N] [--json]
  decision-log version

Provided as-is under the MIT license, use at your own risk.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "add":
		return cmdAdd(args[1:], stdout, stderr)
	case "list":
		return cmdList(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "decision-log %s (MIT license, provided as-is)\n", version)
		return 0
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "decision-log: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
