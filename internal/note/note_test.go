package note

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC)

func valid() *Note {
	return New("Use Redis for session cache", "Multiple app servers", "Sticky sessions (breaks autoscaling)", "user", []string{"src/cache.ts"}, now)
}

func TestValidateAcceptsGoodNote(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]func(n *Note){
		"missing decision": func(n *Note) { n.Decision = "" },
		"missing why":      func(n *Note) { n.Why = "" },
		"missing rejected": func(n *Note) { n.Rejected = "" },
		"bad decided-by":   func(n *Note) { n.DecidedBy = "bot" },
		"too long":         func(n *Note) { n.Why = strings.Repeat("x", MaxLineLen+1) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			n := valid()
			mutate(n)
			if n.Validate() == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestNewFlattensToOneLine(t *testing.T) {
	n := New("  Use\nRedis  ", "a\n\nb", "c", " Claude ", []string{"a.go", "", "a.go", "b.go"}, now)
	if n.Decision != "Use Redis" || n.Why != "a b" {
		t.Fatalf("not flattened: %q / %q", n.Decision, n.Why)
	}
	if n.DecidedBy != ByClaude {
		t.Fatalf("decided-by not normalized: %q", n.DecidedBy)
	}
	if len(n.Files) != 2 {
		t.Fatalf("files not deduped: %v", n.Files)
	}
}

func TestSameDecision(t *testing.T) {
	a, b := valid(), valid()
	a.Repo.RootCommit, b.Repo.RootCommit = "abc", "abc"
	b.Decision = "use redis for session cache."
	if !a.SameDecision(b) {
		t.Fatal("expected duplicate")
	}
	b.Repo.RootCommit = "def"
	if a.SameDecision(b) {
		t.Fatal("different repos must not be duplicates")
	}
}

func TestMergeCombinesFiles(t *testing.T) {
	a, b := valid(), valid()
	b.Why = "newer reason"
	b.Files = []string{"src/cache.ts", "src/session.ts"}
	a.Merge(b, now.Add(time.Hour))
	if a.Why != "newer reason" || len(a.Files) != 2 || !a.UpdatedAt.After(a.CreatedAt) {
		t.Fatalf("bad merge: %+v", a)
	}
}
