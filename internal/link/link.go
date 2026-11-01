// Package link attaches pending notes to the commit that carried them out.
package link

import (
	"slices"
	"time"

	"github.com/decision-log/decision-log/internal/gitx"
	"github.com/decision-log/decision-log/internal/note"
	"github.com/decision-log/decision-log/internal/store"
)

const (
	// Window limits how old a pending note can be and still match a commit. A
	// note past it stays pending rather than risk attaching to unrelated work.
	Window = 72 * time.Hour

	// MaxPerCommit caps notes per commit. Extras stay pending.
	MaxPerCommit = 3
)

// Select returns the notes to link to commit: pending, same repo and branch,
// inside the window and sharing a changed file, newest first. Notes with no
// files never match; they stay standalone.
func Select(notes []*note.Note, repo note.Repo, branch, commit string, changed []string, now time.Time) []*note.Note {
	room := MaxPerCommit
	var out []*note.Note
	for _, n := range notes {
		if n.Commit == commit && n.InRepo(repo.RootCommit, repo.Path) {
			room--
		}
	}
	for _, n := range notes {
		if len(out) >= room {
			break
		}
		age := now.Sub(n.CreatedAt)
		if n.Commit == "" && n.Status == note.StatusActive && n.Branch == branch &&
			n.InRepo(repo.RootCommit, repo.Path) && age >= 0 && age <= Window &&
			sharesFile(n.Files, changed) {
			out = append(out, n)
		}
	}
	return out
}

func sharesFile(files, changed []string) bool {
	return slices.ContainsFunc(files, func(f string) bool { return slices.Contains(changed, f) })
}

// Head links pending notes to the repo's current commit.
func Head(s *store.Store, r *gitx.Repo, now time.Time) error {
	commit, err := r.Head()
	if err != nil {
		return err
	}
	changed, err := r.ChangedFiles(commit)
	if err != nil {
		return err
	}
	// A corrupt note elsewhere shouldn't stop linking, so All's error is ignored.
	notes, _ := s.All()
	root := r.RootCommit()
	selected := Select(notes, note.Repo{RootCommit: root, Path: r.Root}, r.Branch(), commit, changed, now)
	if len(selected) == 0 {
		return nil
	}
	patchID := r.PatchID(commit)
	for _, n := range selected {
		n.Commit, n.PatchID, n.UpdatedAt = commit, patchID, now.UTC()
		// A note made before the repo's first commit has no root commit yet.
		n.Repo.RootCommit = root
		if err := s.Save(n); err != nil {
			return err
		}
	}
	return nil
}
