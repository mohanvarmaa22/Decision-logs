package why

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/decision-log/decision-log/internal/gitx"
	"github.com/decision-log/decision-log/internal/note"
)

type fixture struct {
	t *testing.T
	r *gitx.Repo
}

func newRepo(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t}
	dir := t.TempDir()
	cmd := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	cmd("init", "-q", "-b", "main")
	cmd("config", "user.email", "test@example.com")
	cmd("config", "user.name", "Test")
	cmd("config", "commit.gpgsign", "false")
	r, err := gitx.Find(dir)
	if err != nil {
		t.Fatal(err)
	}
	f.r = r
	return f
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", f.r.Root}, args...)...).CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes the file and commits, returning the new hash.
func (f *fixture) commit(file, body string) string {
	f.t.Helper()
	path := filepath.Join(f.r.Root, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
	f.git("add", ".")
	f.git("commit", "-q", "-m", "change "+file)
	return f.git("rev-parse", "HEAD")
}

// linked returns a note as if the post-commit hook linked it to commit.
func (f *fixture) linked(decision, commit, file string) *note.Note {
	n := note.New(decision, "w", "r", "user", []string{file}, time.Now().Add(-time.Hour))
	n.Repo = note.Repo{RootCommit: f.r.RootCommit(), Path: f.r.Root}
	n.Commit, n.PatchID = commit, f.r.PatchID(commit)
	return n
}

func wantOne(t *testing.T, got []Match, decision, how string) {
	t.Helper()
	if len(got) != 1 || got[0].Note.Decision != decision || got[0].How != how {
		t.Fatalf("want %q by %s, got %+v", decision, how, got)
	}
}

func TestLineByCommit(t *testing.T) {
	f := newRepo(t)
	c := f.commit("a.txt", "one\ntwo\n")
	got, err := Lines(f.r, []*note.Note{f.linked("D", c, "a.txt")}, "a.txt", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	wantOne(t, got, "D", ByCommit)
}

func TestRangeFindsEachCommitOnce(t *testing.T) {
	f := newRepo(t)
	c1 := f.commit("a.txt", "one\ntwo\n")
	c2 := f.commit("a.txt", "one\nTWO\nthree\n")
	notes := []*note.Note{f.linked("first", c1, "a.txt"), f.linked("second", c2, "a.txt")}
	got, err := Lines(f.r, notes, "a.txt", 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestLineSurvivesRebaseByPatchID(t *testing.T) {
	f := newRepo(t)
	f.commit("base.txt", "x\n")
	f.git("switch", "-q", "-c", "feature")
	c := f.commit("a.txt", "one\n")
	n := f.linked("D", c, "a.txt")
	f.git("switch", "-q", "main")
	f.commit("other.txt", "y\n")
	f.git("switch", "-q", "feature")
	f.git("rebase", "-q", "main")
	if f.git("rev-parse", "HEAD") == c {
		t.Fatal("rebase kept the hash")
	}
	got, err := Lines(f.r, []*note.Note{n}, "a.txt", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantOne(t, got, "D", ByPatch)
}

func TestLineFindsSquashByFileAndDate(t *testing.T) {
	f := newRepo(t)
	f.commit("base.txt", "x\n")
	f.git("switch", "-q", "-c", "feature")
	c := f.commit("a.txt", "one\n")
	n := f.linked("D", c, "a.txt")
	f.commit("a.txt", "one\ntwo\n")
	f.git("switch", "-q", "main")
	f.git("merge", "-q", "--squash", "feature")
	f.git("commit", "-q", "-m", "squashed")
	f.git("branch", "-q", "-D", "feature")
	got, err := Lines(f.r, []*note.Note{n}, "a.txt", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantOne(t, got, "D", ByHistory)
}

func TestLineWalksPastUnrelatedChange(t *testing.T) {
	f := newRepo(t)
	c := f.commit("a.txt", "one\n")
	f.commit("a.txt", "ONE\n") // reformatted: the decision is one commit back
	got, err := Lines(f.r, []*note.Note{f.linked("D", c, "a.txt")}, "a.txt", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantOne(t, got, "D", ByCommit)
	if got[0].Commit != c {
		t.Fatalf("wrong commit %s", got[0].Commit)
	}
}

func TestLineRespectsBlameIgnoreRevs(t *testing.T) {
	f := newRepo(t)
	c := f.commit("a.txt", "one\ntwo\n")
	format := f.commit("a.txt", "one\nTWO\n")
	f.commit(".git-blame-ignore-revs", format+"\n")
	got, err := f.r.Blame("a.txt", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != c {
		t.Fatalf("blame = %v, want %s", got, c)
	}
}

func TestLineLeavesOutNoteOnOlderUnrelatedLines(t *testing.T) {
	f := newRepo(t)
	c := f.commit("a.txt", "one\n")
	f.commit("a.txt", "one\ntwo\n")
	// The note's own commit is in this history, so "likely" must not claim it.
	got, err := Lines(f.r, []*note.Note{f.linked("D", c, "a.txt")}, "a.txt", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLineIgnoresOtherRepos(t *testing.T) {
	f := newRepo(t)
	c := f.commit("a.txt", "one\n")
	n := f.linked("D", c, "a.txt")
	n.Repo = note.Repo{RootCommit: "other"}
	if got, _ := Lines(f.r, []*note.Note{n}, "a.txt", 1, 1); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLineErrorsOnUncommittedFile(t *testing.T) {
	f := newRepo(t)
	f.commit("a.txt", "one\n")
	if _, err := Lines(f.r, nil, "new.txt", 1, 1); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := Lines(f.r, nil, "a.txt", 99, 99); err == nil {
		t.Fatal("expected an error past the end of the file")
	}
}

func TestFileFollowsRenameAndPutsActiveFirst(t *testing.T) {
	f := newRepo(t)
	c := f.commit("old.txt", "one\n")
	f.git("mv", "old.txt", "new.txt")
	f.git("commit", "-q", "-m", "rename")
	gone := f.linked("old decision", c, "old.txt")
	gone.Status = note.StatusSuperseded
	now := f.linked("current decision", c, "old.txt")
	got := File(f.r, []*note.Note{gone, now}, "new.txt")
	if len(got) != 2 || got[0].Decision != "current decision" {
		t.Fatalf("got %+v", got)
	}
}

func TestSearch(t *testing.T) {
	f := newRepo(t)
	c := f.commit("a.txt", "one\n")
	here := f.linked("Use Redis for cache", c, "src/cache.ts")
	away := f.linked("Redis in another repo", c, "x.go")
	away.Repo = note.Repo{RootCommit: "other"}
	notes := []*note.Note{away, here}
	if got := Search(f.r, notes, []string{"REDIS"}); len(got) != 2 || got[0] != here {
		t.Fatalf("current repo should come first: %+v", got)
	}
	if got := Search(f.r, notes, []string{"redis", "cache"}); len(got) != 1 || got[0] != here {
		t.Fatalf("every term must match: %+v", got)
	}
	if got := Search(f.r, notes, []string{"cache.ts"}); len(got) != 1 {
		t.Fatalf("should search file names: %+v", got)
	}
	if got := Search(nil, notes, []string{"redis"}); len(got) != 2 {
		t.Fatalf("works outside a repo: %+v", got)
	}
}
