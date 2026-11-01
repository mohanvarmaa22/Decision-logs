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
	if n.Branch != "main" || n.BaseCommit != n.Repo.RootCommit || len(n.Repo.RootCommit) != 40 || n.Status != note.StatusActive || n.Commit != "" {
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
	repo := setup(t) // outside a temp repo this would install into the real one
	code, out, errOut := cli(t, "hook", "session-start")
	if code != 0 || !strings.Contains(out, "decision-log add --decision") || errOut != "" {
		t.Fatalf("got %d %q %q", code, out, errOut)
	}
	hook, err := os.ReadFile(filepath.Join(repo, ".git", "hooks", "post-commit"))
	if err != nil || !strings.Contains(string(hook), "hook post-commit") {
		t.Fatalf("git hook not installed: %v\n%s", err, hook)
	}
}

func TestHookSessionStartOutsideRepo(t *testing.T) {
	t.Setenv(store.HomeEnv, t.TempDir())
	t.Chdir(t.TempDir())
	if code, out, _ := cli(t, "hook", "session-start"); code != 0 || !strings.Contains(out, "decision-log add") {
		t.Fatalf("got %d %q", code, out)
	}
}

func TestHookIsSilentWhenDisabled(t *testing.T) {
	repo := setup(t)
	t.Setenv(DisableEnv, "1")
	if code, out, _ := cli(t, "hook", "session-start"); code != 0 || out != "" {
		t.Fatalf("got %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", "post-commit")); err == nil {
		t.Fatal("hook installed while disabled")
	}
}

func TestHookNeverFails(t *testing.T) {
	for _, args := range [][]string{{"hook"}, {"hook", "unknown-event"}, {"hook", "a", "b"}} {
		if code, out, _ := cli(t, args...); code != 0 || out != "" {
			t.Errorf("%v: got %d %q", args, code, out)
		}
	}
}

func TestPostCommitLinksNote(t *testing.T) {
	repo := setup(t)
	cli(t, addArgs...)
	if err := os.WriteFile(filepath.Join(repo, "src", "cache.ts"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", "cache"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	code, out, errOut := cli(t, "hook", "post-commit")
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("hook not silent: %d %q %q", code, out, errOut)
	}
	if n := listJSON(t); n[0].Commit == "" || n[0].PatchID == "" {
		t.Fatalf("not linked: %+v", n[0])
	}
}

func TestPostCommitFailsOpenOutsideRepo(t *testing.T) {
	t.Setenv(store.HomeEnv, t.TempDir())
	t.Chdir(t.TempDir())
	if code, out, errOut := cli(t, "hook", "post-commit"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("got %d %q %q", code, out, errOut)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestWhyFindsLineFileAndKeyword(t *testing.T) {
	repo := setup(t)
	cli(t, addArgs...)
	if err := os.WriteFile(filepath.Join(repo, "src", "cache.ts"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "cache")
	cli(t, "hook", "post-commit")

	for _, q := range []string{"cache.ts:2", "cache.ts:1-2", "cache.ts", "redis"} {
		code, out, errOut := cli(t, "why", q)
		if code != 0 || !strings.Contains(out, "Use Redis for session cache") || !strings.HasPrefix(out, dataNotice) {
			t.Errorf("why %s: %d %q %q", q, code, out, errOut)
		}
	}
	if _, out, _ := cli(t, "why", "cache.ts:2"); !strings.Contains(out, "Match:     commit ") {
		t.Errorf("line lookup should say how it matched: %s", out)
	}
}

func TestWhyWithNoResults(t *testing.T) {
	repo := setup(t)
	if err := os.WriteFile(filepath.Join(repo, "src", "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "a")
	cases := map[string]string{"a.txt:1": "No decision linked", "a.txt": "No decisions about", "nothing": "No decisions found"}
	for q, want := range cases {
		if code, out, _ := cli(t, "why", q); code != 0 || !strings.Contains(out, want) {
			t.Errorf("why %s: %d %q", q, code, out)
		}
	}
}

func TestWhyRejectsBadInput(t *testing.T) {
	setup(t)
	if code, _, _ := cli(t, "why"); code != 2 {
		t.Errorf("no args: exit %d", code)
	}
	if code, _, errOut := cli(t, "why", "a.txt:5-2"); code != 2 || !strings.Contains(errOut, "bad line range") {
		t.Errorf("bad range: %d %q", code, errOut)
	}
	if code, _, _ := cli(t, "why", "missing.txt:1"); code != 1 {
		t.Errorf("missing file: exit %d", code)
	}
}

func TestSupersedes(t *testing.T) {
	setup(t)
	_, out, _ := cli(t, addArgs...)
	old := strings.TrimSpace(strings.TrimPrefix(out, "decision-log: saved "))

	if code, _, errOut := cli(t, "add", "--decision", "Use Valkey", "--why", "w", "--rejected", "r", "--by", "user", "--supersedes", "nope"); code != 2 || !strings.Contains(errOut, "no earlier note") {
		t.Fatalf("unknown id: %d %q", code, errOut)
	}
	if n := listJSON(t); len(n) != 1 {
		t.Fatalf("note saved despite a bad id: %d", len(n))
	}

	code, _, errOut := cli(t, "add", "--decision", "Use Valkey", "--why", "w", "--rejected", "Redis", "--by", "user", "--supersedes", old)
	if code != 0 {
		t.Fatalf("add failed: %s", errOut)
	}
	notes := listJSON(t)
	var oldNote, newNote note.Note
	for _, n := range notes {
		if n.ID == old {
			oldNote = n
		} else {
			newNote = n
		}
	}
	if oldNote.Status != note.StatusSuperseded || oldNote.SupersededBy != newNote.ID || newNote.Status != note.StatusActive {
		t.Fatalf("not superseded: %+v / %+v", oldNote, newNote)
	}
	if _, text, _ := cli(t, "list"); !strings.Contains(text, "[superseded]") || !strings.Contains(text, "Replaced:  by "+newNote.ID) {
		t.Fatalf("list doesn't show it:\n%s", text)
	}
}
