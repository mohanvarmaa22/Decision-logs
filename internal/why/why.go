// Package why finds the decisions behind lines, files and keywords.
package why

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/decision-log/decision-log/internal/gitx"
	"github.com/decision-log/decision-log/internal/note"
)

// How a note was matched to a commit, strongest first.
const (
	ByCommit  = "commit" // the note is linked to this commit
	ByPatch   = "patch"  // same change under a new hash: rebase or cherry-pick
	ByHistory = "likely" // same file and a plausible date: e.g. a squash merge
)

const (
	// maxWalk bounds how far back a line's history is searched.
	maxWalk = 50
	// likelyWindow is how long after a note a squash merge can plausibly land.
	likelyWindow = 30 * 24 * time.Hour
)

// Match is a note found for some lines, and the commit that led to it.
type Match struct {
	Note   *note.Note
	How    string
	Commit string
}

// Lines finds the decisions behind lines first to last of a repo-relative file.
// It tries the commits that last touched the lines, then the lines' older
// history in case the latest change was unrelated, such as formatting. Only if
// neither has an exact match does it fall back to file and date.
func Lines(r *gitx.Repo, notes []*note.Note, file string, first, last int) ([]Match, error) {
	notes = inRepo(r, notes)
	blamed, err := r.Blame(file, first, last)
	if err != nil {
		return nil, err
	}
	if m := exact(r, notes, blamed); len(m) > 0 {
		return m, nil
	}
	history := r.LineHistory(file, first, last, maxWalk)
	// Walk newest first and stop at the first commit that has a decision.
	for _, c := range history {
		if m := exact(r, notes, []string{c}); len(m) > 0 {
			return m, nil
		}
	}
	for _, c := range append(blamed, history...) {
		if m := likely(r, notes, c); len(m) > 0 {
			return m, nil
		}
	}
	return nil, nil
}

// exact matches by commit ID, then patch ID. Each note appears once.
func exact(r *gitx.Repo, notes []*note.Note, commits []string) []Match {
	var out []Match
	seen := map[string]bool{}
	for _, c := range commits {
		patch := ""
		for _, n := range notes {
			if seen[n.ID] {
				continue
			}
			how := ""
			if n.Commit == c {
				how = ByCommit
			} else if n.PatchID != "" {
				if patch == "" {
					patch = r.PatchID(c)
				}
				if n.PatchID == patch {
					how = ByPatch
				}
			}
			if how != "" {
				seen[n.ID] = true
				out = append(out, Match{n, how, c})
			}
		}
	}
	return activeFirst(out)
}

// likely matches a commit that touched a note's file soon after the note. A note
// already linked to a commit in this history isn't about this one, so it's skipped.
func likely(r *gitx.Repo, notes []*note.Note, commit string) []Match {
	changed, err := r.ChangedFiles(commit)
	if err != nil {
		return nil
	}
	at, err := r.CommitTime(commit)
	if err != nil {
		return nil
	}
	var out []Match
	for _, n := range notes {
		age := at.Sub(n.CreatedAt.Truncate(time.Second))
		if age < 0 || age > likelyWindow || !slices.ContainsFunc(n.Files, func(f string) bool { return slices.Contains(changed, f) }) {
			continue
		}
		if n.Commit != "" && r.IsAncestor(n.Commit, "HEAD") {
			continue
		}
		out = append(out, Match{n, ByHistory, commit})
	}
	return activeFirst(out)
}

// File returns every decision about a repo-relative file, under any earlier name.
func File(r *gitx.Repo, notes []*note.Note, file string) []*note.Note {
	names := append(r.FileNames(file), file)
	var out []*note.Note
	for _, n := range inRepo(r, notes) {
		if slices.ContainsFunc(n.Files, func(f string) bool { return slices.Contains(names, f) }) {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// Search returns notes containing every term, ignoring case. Notes from repo
// come first; repo may be nil.
func Search(r *gitx.Repo, notes []*note.Note, terms []string) []*note.Note {
	var out []*note.Note
	for _, n := range notes {
		text := strings.ToLower(strings.Join(append([]string{n.Decision, n.Why, n.Rejected}, n.Files...), " "))
		if !slices.ContainsFunc(terms, func(t string) bool { return !strings.Contains(text, strings.ToLower(t)) }) {
			out = append(out, n)
		}
	}
	if r != nil {
		root := r.RootCommit()
		here := func(n *note.Note) bool { return n.InRepo(root, r.Root) }
		sort.SliceStable(out, func(i, j int) bool { return here(out[i]) && !here(out[j]) })
	}
	return out
}

func inRepo(r *gitx.Repo, notes []*note.Note) []*note.Note {
	root := r.RootCommit()
	return slices.DeleteFunc(slices.Clone(notes), func(n *note.Note) bool { return !n.InRepo(root, r.Root) })
}

func rank(n *note.Note) int {
	if n.Status == note.StatusSuperseded {
		return 1
	}
	return 0
}

func activeFirst(m []Match) []Match {
	sort.SliceStable(m, func(i, j int) bool { return rank(m[i].Note) < rank(m[j].Note) })
	return m
}
