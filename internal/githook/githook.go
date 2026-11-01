// Package githook installs decision-log's post-commit hook by adding one marked
// block to the repo's hook. A user's existing hook keeps running, and removing
// the block restores it byte for byte.
package githook

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/decision-log/decision-log/internal/gitx"
)

const (
	beginMarker = "# >>> decision-log >>>"
	endMarker   = "# <<< decision-log <<<"
	newHook     = "#!/bin/sh\n"
)

// Installing into any of these could break or leak into someone else's setup,
// so Install leaves them alone and says why.
var (
	ErrNotShell   = errors.New("post-commit hook isn't a shell script")
	ErrSymlink    = errors.New("post-commit hook is a symlink, possibly shared")
	ErrDisabled   = errors.New("post-commit hook exists but isn't executable, so it's disabled")
	ErrNotPrivate = errors.New("hooks directory is shared or tracked in git")
	ErrDamaged    = errors.New("post-commit hook has a damaged decision-log block")
)

var shellShebang = regexp.MustCompile(`^#!\s*(\S*/)?(env\s+(-S\s+)?)?(sh|bash|dash|zsh|ksh)\b`)

// Install adds or updates the block in r's post-commit hook so that it runs
// bin, and records the hook under home so uninstall can find it later. changed
// is false when the block was already current. It's safe to call every session.
func Install(r *gitx.Repo, bin, home string) (changed bool, err error) {
	path, err := hookPath(r)
	if err != nil {
		return false, err
	}
	mode := os.FileMode(0o755)
	content := ""
	switch fi, err := os.Lstat(path); {
	case err == nil && fi.Mode()&os.ModeSymlink != 0:
		return false, ErrSymlink
	case err == nil && fi.Mode().Perm()&0o111 == 0:
		return false, ErrDisabled
	case err == nil:
		mode = fi.Mode().Perm()
		data, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		content = string(data)
	case !errors.Is(err, os.ErrNotExist):
		return false, err
	}

	updated, err := withBlock(content, bin)
	if err != nil {
		return false, err
	}
	if err := record(home, path); err != nil {
		return false, err
	}
	if updated == content {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, writeAtomic(path, updated, mode)
}

// Remove takes the block out of a hook file. If only the bare hook this package
// created is left, the file goes too.
func Remove(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	content := string(data)
	before, after, found, err := splitBlock(content)
	if err != nil || !found {
		return err
	}
	rest := before + after
	if rest == newHook {
		return os.Remove(path)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	return writeAtomic(path, rest, fi.Mode().Perm())
}

// hookPath finds the post-commit path, refusing locations that aren't this
// repo's own: a global core.hooksPath, or a hooks directory tracked in git,
// where a new or changed file would dirty the repo or reach teammates.
func hookPath(r *gitx.Repo) (string, error) {
	dir, err := r.HooksDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "post-commit")
	if r.InGitDir(dir) || r.IsIgnored(path) {
		return path, nil
	}
	return "", ErrNotPrivate
}

// withBlock puts the block right after the shebang, not at the end, because an
// existing hook may finish with `exit` and our block would never run.
func withBlock(content, bin string) (string, error) {
	block := renderBlock(bin)
	before, after, found, err := splitBlock(content)
	if err != nil {
		return "", err
	}
	if found {
		return before + block + after, nil
	}
	if content == "" {
		return newHook + block, nil
	}
	shebang, rest, hasNewline := strings.Cut(content, "\n")
	if !shellShebang.MatchString(shebang) {
		return "", ErrNotShell
	}
	if !hasNewline {
		return "", ErrNotShell // nothing after the shebang to keep byte for byte
	}
	return shebang + "\n" + block + rest, nil
}

func renderBlock(bin string) string {
	// An absolute path, because Git GUIs often run hooks without the user's PATH.
	// Each session rewrites it, so an upgrade that moves the binary heals itself.
	quoted := "'" + strings.ReplaceAll(bin, "'", `'\''`) + "'"
	return fmt.Sprintf("%s\n%s hook post-commit >/dev/null 2>&1 || true\n%s\n", beginMarker, quoted, endMarker)
}

// splitBlock returns the text around the block, whole lines only.
func splitBlock(content string) (before, after string, found bool, err error) {
	start := strings.Index(content, beginMarker+"\n")
	end := strings.Index(content, endMarker+"\n")
	if start < 0 && end < 0 {
		return content, "", false, nil
	}
	if start < 0 || end < start {
		return "", "", false, ErrDamaged
	}
	return content[:start], content[end+len(endMarker)+1:], true, nil
}

// record leaves one file per hook, so parallel sessions can't collide. Each
// holds the hook's path for uninstall.
func record(home, hook string) error {
	dir := filepath.Join(home, "hooks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(hook))
	file := filepath.Join(dir, hex.EncodeToString(sum[:8]))
	if _, err := os.Stat(file); err == nil {
		return nil
	}
	return writeAtomic(file, hook+"\n", 0o600)
}

func writeAtomic(path, content string, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".decision-log-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
