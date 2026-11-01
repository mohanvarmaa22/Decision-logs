// Package gitx runs the few read-only git commands decision-log needs.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Timeout bounds every git call so a slow repo can never stall the caller.
const Timeout = 2 * time.Second

// ErrNotRepo means the directory is not inside a git work tree.
var ErrNotRepo = errors.New("not a git repository")

// Repo is a git work tree.
type Repo struct {
	Root string // absolute, symlinks resolved
}

// Find returns the repo containing dir.
func Find(dir string) (*Repo, error) {
	out, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ErrNotRepo
	}
	return &Repo{Root: resolve(out)}, nil
}

// RootCommit is the oldest root commit, or "" in a repo with no commits yet.
func (r *Repo) RootCommit() string {
	out, err := run(r.Root, "rev-list", "--max-parents=0", "HEAD")
	if err != nil || out == "" {
		return ""
	}
	lines := strings.Split(out, "\n")
	return lines[len(lines)-1]
}

// Remote is origin's URL, or "" if there is none.
func (r *Repo) Remote() string {
	out, _ := run(r.Root, "remote", "get-url", "origin")
	return out
}

// Branch is the current branch, or "" when HEAD is detached.
func (r *Repo) Branch() string {
	out, _ := run(r.Root, "symbolic-ref", "--quiet", "--short", "HEAD")
	return out
}

// Head is the current commit's full hash.
func (r *Repo) Head() (string, error) {
	return run(r.Root, "rev-parse", "HEAD")
}

// IsAncestor reports whether commit descends from (or is) base.
func (r *Repo) IsAncestor(base, commit string) bool {
	_, err := run(r.Root, "merge-base", "--is-ancestor", base, commit)
	return err == nil
}

// ChangedFiles lists the repo-relative files a commit changed. Renames appear
// under both names. A merge commit lists none.
func (r *Repo) ChangedFiles(commit string) ([]string, error) {
	out, err := run(r.Root, "diff-tree", "--no-commit-id", "--name-only", "-r", "-z", "--root", commit)
	if err != nil {
		return nil, err
	}
	return strings.FieldsFunc(out, func(c rune) bool { return c == 0 }), nil
}

// PatchID is the stable fingerprint of a commit's change, or "" if it has none.
func (r *Repo) PatchID(commit string) string {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	diff := exec.CommandContext(ctx, "git", "-C", r.Root, "diff-tree", "-p", "--root", commit)
	patch := exec.CommandContext(ctx, "git", "-C", r.Root, "patch-id", "--stable")
	var err error
	if patch.Stdin, err = diff.StdoutPipe(); err != nil {
		return ""
	}
	var stdout bytes.Buffer
	patch.Stdout = &stdout
	if err := diff.Start(); err != nil {
		return ""
	}
	patchErr := patch.Run()
	if diff.Wait() != nil || patchErr != nil {
		return ""
	}
	id, _, _ := strings.Cut(stdout.String(), " ")
	return strings.TrimSpace(id)
}

// RelPath turns a path given relative to cwd (or absolute) into a
// slash-separated path relative to the repo root. Paths outside the repo come
// back cleaned but otherwise unchanged.
func (r *Repo) RelPath(cwd, p string) string {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, p)
	}
	abs = resolve(abs)
	rel, err := filepath.Rel(r.Root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(filepath.Clean(p))
	}
	return filepath.ToSlash(rel)
}

// resolve follows symlinks (macOS /tmp is /private/tmp, for example) so paths
// compare equal to git's. For a path that doesn't exist yet, the nearest
// existing ancestor is resolved and the rest is appended.
func resolve(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for dir := p; ; dir = filepath.Dir(dir) {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

func run(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
