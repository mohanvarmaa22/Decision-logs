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

func pending(mutate ...func(*note.Note)) *note.Note {
	n := note.New("Use Redis", "w", "r", "user", []string{"src/cache.ts"}, now.Add(-time.Hour))
	n.Repo, n.Branch = repo, "main"
	for _, m := range mutate {
		m(n)
	}
	return n
}

func TestSelectMatches(t *testing.T) {
	got := Select([]*note.Note{pending()}, repo, "main", "c1", []string{"a.go", "src/cache.ts"}, now)
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
			if got := Select([]*note.Note{pending(mutate)}, repo, "main", "c1", []string{"src/cache.ts"}, now); len(got) != 0 {
				t.Fatalf("selected %+v", got[0])
			}
		})
	}
}

func TestSelectCapsPerCommit(t *testing.T) {
	notes := []*note.Note{pending(), pending(), pending(), pending()}
	if got := Select(notes, repo, "main", "c1", []string{"src/cache.ts"}, now); len(got) != MaxPerCommit {
		t.Fatalf("got %d notes", len(got))
	}
}

func TestSelectCountsNotesAlreadyOnCommit(t *testing.T) {
	linked := func(n *note.Note) { n.Commit = "c1" }
	notes := []*note.Note{pending(linked), pending(linked), pending(), pending()}
	if got := Select(notes, repo, "main", "c1", []string{"src/cache.ts"}, now); len(got) != 1 {
		t.Fatalf("got %d notes", len(got))
	}
}

func TestSelectMatchesNoteMadeBeforeFirstCommit(t *testing.T) {
	early := pending(func(n *note.Note) { n.Repo.RootCommit = "" })
	if got := Select([]*note.Note{early}, repo, "main", "c1", []string{"src/cache.ts"}, now); len(got) != 1 {
		t.Fatalf("got %d notes", len(got))
	}
}

func TestHeadLinksRealCommit(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(dir, "cache.ts"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := gitx.Find(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	early := time.Now().Add(-time.Minute)
	n := note.New("Use Redis", "w", "r", "user", []string{"cache.ts"}, early)
	n.Repo, n.Branch = note.Repo{Path: r.Root}, "main"
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}

	git("add", ".")
	git("commit", "-q", "-m", "first")
	if err := Head(s, r, time.Now()); err != nil {
		t.Fatal(err)
	}

	notes, _ := s.All()
	got := notes[0]
	head, _ := r.Head()
	if got.Commit != head || got.PatchID == "" || got.Repo.RootCommit != head {
		t.Fatalf("not linked: %+v", got)
	}
}
