package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/decision-log/decision-log/internal/note"
	"github.com/decision-log/decision-log/internal/store"
)

// setup gives each test its own data dir and a git repo as the working dir.
func setup(t *testing.T) (repo string) {
	t.Helper()
	t.Setenv(store.HomeEnv, t.TempDir())
	repo = t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"commit", "-q", "--allow-empty", "-m", "root"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(repo, "src"))
	return repo
}

func cli(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func listJSON(t *testing.T, args ...string) []note.Note {
	t.Helper()
	code, out, errOut := cli(t, append([]string{"list", "--json"}, args...)...)
	if code != 0 {
		t.Fatalf("list failed: %s", errOut)
	}
	var notes []note.Note
	if err := json.Unmarshal([]byte(out), &notes); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	return notes
}

var addArgs = []string{
	"add",
	"--decision", "Use Redis for session cache",
	"--why", "Multiple app servers; in-memory cache isn't shared",
	"--rejected", "Sticky sessions (breaks autoscaling)",
	"--by", "user",
	"--files", "cache.ts,session.ts",
}

func TestAddThenList(t *testing.T) {
	setup(t)
	code, out, errOut := cli(t, addArgs...)
	if code != 0 {
		t.Fatalf("add failed (%d): %s", code, errOut)
	}
	if !strings.HasPrefix(out, "decision-log: saved ") || strings.Count(out, "\n") != 1 {
		t.Fatalf("want one short line, got %q", out)
	}

	notes := listJSON(t, "--here")
	if len(notes) != 1 {
		t.Fatalf("want 1 note, got %d", len(notes))
	}
	n := notes[0]
	if got := strings.Join(n.Files, ","); got != "src/cache.ts,src/session.ts" {
		t.Errorf("files not repo-relative: %s", got)
	}
	if n.Branch != "main" || len(n.Repo.RootCommit) != 40 || n.Status != note.StatusActive || n.Commit != "" {
		t.Errorf("bad metadata: %+v", n)
	}

	_, text, _ := cli(t, "list")
	for _, want := range []string{"Use Redis for session cache", "Why:", "Rejected:", "src/cache.ts", "pending"} {
		if !strings.Contains(text, want) {
			t.Errorf("text list missing %q:\n%s", want, text)
		}
	}
}

func TestDuplicateIsMerged(t *testing.T) {
	setup(t)
	cli(t, addArgs...)
	now = func() time.Time { return time.Now().Add(time.Minute) }
	t.Cleanup(func() { now = time.Now })

	code, out, _ := cli(t, "add", "--decision", "use redis for session cache.", "--why", "newer reason",
		"--rejected", "Sticky sessions", "--by", "claude", "--files", "redis.ts")
	if code != 0 || !strings.Contains(out, "updated") {
		t.Fatalf("want update, got %d %q", code, out)
	}
	notes := listJSON(t)
	if len(notes) != 1 || notes[0].Why != "newer reason" || len(notes[0].Files) != 3 {
		t.Fatalf("bad merge: %+v", notes)
	}
}

func TestAddRejectsBadNote(t *testing.T) {
	setup(t)
	code, _, errOut := cli(t, "add", "--decision", "x", "--by", "someone")
	if code != 2 {
		t.Fatalf("want exit 2, got %d", code)
	}
	for _, want := range []string{"--why is required", "--rejected is required", "--by must be"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("error missing %q: %s", want, errOut)
		}
	}
	if n := listJSON(t); len(n) != 0 {
		t.Fatal("invalid note was saved")
	}
}

func TestAddRedactsSecrets(t *testing.T) {
	setup(t)
	code, out, _ := cli(t, "add", "--decision", "Move key out of code", "--why", "AKIAIOSFODNN7EXAMPLE was hardcoded",
		"--rejected", "Keep it in config", "--by", "user")
	if code != 0 || !strings.Contains(out, "secrets redacted") {
		t.Fatalf("got %d %q", code, out)
	}
	if n := listJSON(t); strings.Contains(n[0].Why, "AKIA") {
		t.Fatalf("secret stored: %q", n[0].Why)
	}
}

func TestAddOutsideRepo(t *testing.T) {
	t.Setenv(store.HomeEnv, t.TempDir())
	t.Chdir(t.TempDir())
	code, _, errOut := cli(t, addArgs...)
	if code != 0 {
		t.Fatalf("add failed: %s", errOut)
	}
	n := listJSON(t)
	if len(n) != 1 || n[0].Repo.RootCommit != "" || n[0].Repo.Path != "" {
		t.Fatalf("want standalone note, got %+v", n)
	}
}

func TestStrayArgument(t *testing.T) {
	setup(t)
	code, _, errOut := cli(t, "add", "--decision", "Use", "Redis")
	if code != 2 || !strings.Contains(errOut, "quote each value") {
		t.Fatalf("got %d %q", code, errOut)
	}
}

func TestHookSessionStart(t *testing.T) {
	code, out, _ := cli(t, "hook", "session-start")
	if code != 0 || !strings.Contains(out, "decision-log add --decision") {
		t.Fatalf("got %d %q", code, out)
	}
}

func TestHookIsSilentWhenDisabled(t *testing.T) {
	t.Setenv(DisableEnv, "1")
	if code, out, _ := cli(t, "hook", "session-start"); code != 0 || out != "" {
		t.Fatalf("got %d %q", code, out)
	}
}

func TestHookNeverFails(t *testing.T) {
	for _, args := range [][]string{{"hook"}, {"hook", "unknown-event"}, {"hook", "a", "b"}} {
		if code, out, _ := cli(t, args...); code != 0 || out != "" {
			t.Errorf("%v: got %d %q", args, code, out)
		}
	}
}
