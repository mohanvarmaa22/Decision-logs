package link

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/decision-log/decision-log/internal/gitx"
	"github.com/decision-log/decision-log/internal/note"
	"github.com/decision-log/decision-log/internal/store"
)

var (
	now  = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	repo = note.Repo{RootCommit: "root", Path: "/repo"}
)

func never(string) bool { return false }

func pending(mutate ...func(*note.Note)) *note.Note {
	n := note.New("Use Redis", "w", "r", "user", []string{"src/cache.ts"}, now.Add(-time.Hour))
	n.Repo, n.Branch = repo, "main"
	for _, m := range mutate {
		m(n)
	}
	return n
}

func TestSelectMatches(t *testing.T) {
	got := Select([]*note.Note{pending()}, repo, "main", "c1", []string{"a.go", "src/cache.ts"}, never, now)
	if len(got) != 1 {
		t.Fatalf("got %d notes", len(got))
	}
}

func TestSelectSkips(t *testing.T) {
	cases := map[string]func(*note.Note){
		"different file":   func(n *note.Note) { n.Files = []string{"other.go"} },
		"no files":         func(n *note.Note) { n.Files = nil },
		"different branch": func(n *note.Note) { n.Branch = "feature" },
		"different repo":   func(n *note.Note) { n.Repo = note.Repo{RootCommit: "other", Path: "/other"} },
		"already linked":   func(n *note.Note) { n.Commit = "c0" },
		"superseded":       func(n *note.Note) { n.Status = note.StatusSuperseded },
		"too old":          func(n *note.Note) { n.CreatedAt = now.Add(-Window - time.Minute) },
		"from the future":  func(n *note.Note) { n.CreatedAt = now.Add(time.Minute) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Select([]*note.Note{pending(mutate)}, repo, "main", "c1", []string{"src/cache.ts"}, never, now); len(got) != 0 {
				t.Fatalf("selected %+v", got[0])
			}
		})
	}
}

func TestSelectFollowsHistoryAcrossBranches(t *testing.T) {
	n := pending(func(n *note.Note) { n.BaseCommit = "base" })
	descends := func(base string) bool { return base == "base" }
	if got := Select([]*note.Note{n}, repo, "feature", "c1", []string{"src/cache.ts"}, descends, now); len(got) != 1 {
		t.Fatalf("got %d notes", len(got))
	}
}

func TestSelectCapsPerCommit(t *testing.T) {
	notes := []*note.Note{pending(), pending(), pending(), pending()}
	if got := Select(notes, repo, "main", "c1", []string{"src/cache.ts"}, never, now); len(got) != MaxPerCommit {
		t.Fatalf("got %d notes", len(got))
	}
}

func TestSelectCountsNotesAlreadyOnCommit(t *testing.T) {
	linked := func(n *note.Note) { n.Commit = "c1" }
	notes := []*note.Note{pending(linked), pending(linked), pending(), pending()}
	if got := Select(notes, repo, "main", "c1", []string{"src/cache.ts"}, never, now); len(got) != 1 {
		t.Fatalf("got %d notes", len(got))
	}
}

func TestSelectMatchesNoteMadeBeforeFirstCommit(t *testing.T) {
	early := pending(func(n *note.Note) { n.Repo.RootCommit = "" })
	if got := Select([]*note.Note{early}, repo, "main", "c1", []string{"src/cache.ts"}, never, now); len(got) != 1 {
		t.Fatalf("got %d notes", len(got))
	}
}

// realRepo returns a repo with a git helper, and an empty store.
func realRepo(t *testing.T) (*gitx.Repo, *store.Store, func(...string)) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")
	r, err := gitx.Find(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return r, s, git
}

func commitFile(t *testing.T, r *gitx.Repo, git func(...string), name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Root, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", name)
}

func saved(t *testing.T, s *store.Store, r *gitx.Repo, branch, base, file string) {
	t.Helper()
	n := note.New("Use Redis", "w", "r", "user", []string{file}, time.Now().Add(-time.Minute))
	n.Repo, n.Branch, n.BaseCommit = note.Repo{RootCommit: r.RootCommit(), Path: r.Root}, branch, base
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}
}

func commitOf(t *testing.T, s *store.Store) string {
	t.Helper()
	notes, err := s.All()
	if err != nil || len(notes) != 1 {
		t.Fatalf("notes=%d err=%v", len(notes), err)
	}
	return notes[0].Commit
}

func TestHeadLinksRealCommit(t *testing.T) {
	r, s, git := realRepo(t)
	saved(t, s, r, "main", "", "cache.ts") // no commits yet, so no root or base
	commitFile(t, r, git, "cache.ts")
	if err := Head(s, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	notes, _ := s.All()
	head, _ := r.Head()
	if got := notes[0]; got.Commit != head || got.PatchID == "" || got.Repo.RootCommit != head {
		t.Fatalf("not linked: %+v", got)
	}
}

func TestHeadLinksOnBranchCreatedAfterNote(t *testing.T) {
	r, s, git := realRepo(t)
	commitFile(t, r, git, "a.txt")
	base, _ := r.Head()
	saved(t, s, r, "main", base, "cache.ts")
	git("switch", "-q", "-c", "feature")
	commitFile(t, r, git, "cache.ts")
	if err := Head(s, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if head, _ := r.Head(); commitOf(t, s) != head {
		t.Fatal("note not linked on the new branch")
	}
}

func TestHeadSkipsBranchFromBeforeNote(t *testing.T) {
	r, s, git := realRepo(t)
	commitFile(t, r, git, "a.txt")
	git("branch", "other-work")
	commitFile(t, r, git, "b.txt")
	base, _ := r.Head()
	saved(t, s, r, "main", base, "cache.ts")
	git("switch", "-q", "other-work")
	commitFile(t, r, git, "cache.ts")
	if err := Head(s, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := commitOf(t, s); got != "" {
		t.Fatalf("linked to unrelated branch: %s", got)
	}
}

func TestHeadLinksOnSameBranchAfterHistoryRewrite(t *testing.T) {
	r, s, git := realRepo(t)
	commitFile(t, r, git, "root.txt") // amending the first commit would change the repo's identity
	commitFile(t, r, git, "a.txt")
	base, _ := r.Head()
	saved(t, s, r, "main", base, "cache.ts")
	git("commit", "-q", "--amend", "-m", "reworded") // base is no longer an ancestor
	commitFile(t, r, git, "cache.ts")
	if err := Head(s, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if head, _ := r.Head(); commitOf(t, s) != head {
		t.Fatal("note not linked on its own branch")
	}
}
