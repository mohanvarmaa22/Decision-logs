// Package note defines a decision note and the quality rules it must pass.
package note

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// SchemaVersion is bumped whenever the on-disk JSON shape changes.
const SchemaVersion = 1

// MaxLineLen caps decision, why and rejected. Notes are one line each, not essays.
const MaxLineLen = 200

const (
	ByUser   = "user"
	ByClaude = "claude"

	StatusActive     = "active"
	StatusSuperseded = "superseded"
)

// Repo identifies the repository a note belongs to. RootCommit plus Remote is
// the stable identity; Path is only a hint, since clones can move.
type Repo struct {
	RootCommit string `json:"root_commit,omitempty"`
	Remote     string `json:"remote,omitempty"`
	Path       string `json:"path,omitempty"`
}

// Note is one decision. It records files, never line numbers: lines shift,
// and git tracks exact lines once the note is linked to a commit.
type Note struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Decision  string   `json:"decision"`
	Why       string   `json:"why"`
	Rejected  string   `json:"rejected"`
	DecidedBy string   `json:"decided_by"`
	Files     []string `json:"files,omitempty"`

	Repo   Repo   `json:"repo"`
	Branch string `json:"branch,omitempty"`

	Status       string `json:"status"`
	SupersededBy string `json:"superseded_by,omitempty"`

	// Commit is empty while the note is pending, i.e. not yet linked.
	Commit string `json:"commit,omitempty"`
}

// New returns a note with an ID and timestamps set and its fields cleaned up.
func New(decision, why, rejected, decidedBy string, files []string, now time.Time) *Note {
	now = now.UTC()
	return &Note{
		Version:   SchemaVersion,
		ID:        newID(now),
		CreatedAt: now,
		UpdatedAt: now,
		Decision:  clean(decision),
		Why:       clean(why),
		Rejected:  clean(rejected),
		DecidedBy: strings.ToLower(strings.TrimSpace(decidedBy)),
		Files:     CleanFiles(files),
		Status:    StatusActive,
	}
}

// Validate enforces the format rules. Errors are short because Claude reads
// them and retries.
func (n *Note) Validate() error {
	var errs []error
	for _, f := range []struct{ name, val string }{
		{"decision", n.Decision},
		{"why", n.Why},
		{"rejected", n.Rejected},
	} {
		switch {
		case f.val == "":
			errs = append(errs, fmt.Errorf("--%s is required", f.name))
		case utf8.RuneCountInString(f.val) > MaxLineLen:
			errs = append(errs, fmt.Errorf("--%s is over %d characters; keep it to one short line", f.name, MaxLineLen))
		}
	}
	if n.DecidedBy != ByUser && n.DecidedBy != ByClaude {
		errs = append(errs, fmt.Errorf("--by must be %q or %q", ByUser, ByClaude))
	}
	return errors.Join(errs...)
}

// SameDecision reports whether other is a duplicate of n: same repo and the
// same decision text, ignoring case, spacing and trailing punctuation.
func (n *Note) SameDecision(other *Note) bool {
	return n.repoKey() == other.repoKey() && normalize(n.Decision) == normalize(other.Decision)
}

// Merge folds a duplicate into n. The newer reason wins and files are combined.
func (n *Note) Merge(dup *Note, now time.Time) {
	n.Why = dup.Why
	n.Rejected = dup.Rejected
	n.DecidedBy = dup.DecidedBy
	n.Files = CleanFiles(append(n.Files, dup.Files...))
	n.UpdatedAt = now.UTC()
}

func (n *Note) repoKey() string {
	if n.Repo.RootCommit != "" {
		return n.Repo.RootCommit
	}
	return n.Repo.Path
}

// clean joins the text onto one line and trims it.
func clean(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func normalize(s string) string {
	return strings.TrimRight(strings.ToLower(clean(s)), ".!;: ")
}

// CleanFiles trims paths and drops blanks and duplicates, keeping order.
func CleanFiles(files []string) []string {
	seen := make(map[string]bool, len(files))
	var out []string
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// newID sorts by time and is unique across parallel sessions.
func newID(now time.Time) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return now.Format("20060102T150405Z") + "-" + hex.EncodeToString(b)
}
