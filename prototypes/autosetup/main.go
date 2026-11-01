// Command autosetup is a throwaway probe for prototype test 3: can the installer
// add and remove its allow rule in a user's Claude Code settings safely? It takes
// an explicit path and never defaults to the real settings file.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

const allowRule = "Bash(decision-log add:*)"

// field and object keep keys in file order, so a user's settings don't get
// reshuffled by a one-line change. Values stay raw for the same reason.
type field struct {
	key string
	val json.RawMessage
}

type object []field

func parseObject(data []byte) (object, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if tok != json.Delim('{') {
		return nil, errors.New("expected a JSON object")
	}
	var obj object
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		obj = append(obj, field{k.(string), v})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected content after the JSON object")
	}
	return obj, nil
}

func (o object) get(key string) (json.RawMessage, bool) {
	for _, f := range o {
		if f.key == key {
			return f.val, true
		}
	}
	return nil, false
}

func (o object) set(key string, val json.RawMessage) object {
	for i, f := range o {
		if f.key == key {
			o[i].val = val
			return o
		}
	}
	return append(o, field{key, val})
}

func (o object) del(key string) object {
	return slices.DeleteFunc(o, func(f field) bool { return f.key == key })
}

func (o object) marshal() (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(f.key)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(f.val)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// loadAllow returns the settings, their permissions object and its allow list.
// It refuses a file it doesn't fully understand instead of risking the user's
// settings: invalid JSON, or permissions/allow of an unexpected type.
func loadAllow(data []byte) (settings, perms object, allow []json.RawMessage, err error) {
	if settings, err = parseObject(data); err != nil {
		return nil, nil, nil, fmt.Errorf("settings are not plain JSON: %w", err)
	}
	if raw, ok := settings.get("permissions"); ok {
		if perms, err = parseObject(raw); err != nil {
			return nil, nil, nil, fmt.Errorf("permissions: %w", err)
		}
	}
	if raw, ok := perms.get("allow"); ok {
		if err = json.Unmarshal(raw, &allow); err != nil {
			return nil, nil, nil, fmt.Errorf("permissions.allow: %w", err)
		}
	}
	return settings, perms, allow, nil
}

func hasRule(allow []json.RawMessage) int {
	return slices.IndexFunc(allow, func(r json.RawMessage) bool {
		var s string
		return json.Unmarshal(r, &s) == nil && s == allowRule
	})
}

// addRule returns the new file content; changed is false if the rule exists.
func addRule(data []byte) (out []byte, changed bool, err error) {
	settings, perms, allow, err := loadAllow(data)
	if err != nil {
		return nil, false, err
	}
	if hasRule(allow) >= 0 {
		return data, false, nil
	}
	rule, _ := json.Marshal(allowRule)
	allow = append(allow, rule)
	return render(settings, perms, allow)
}

// removeRule removes only this tool's rule, plus any container left empty.
func removeRule(data []byte) (out []byte, changed bool, err error) {
	settings, perms, allow, err := loadAllow(data)
	if err != nil {
		return nil, false, err
	}
	i := hasRule(allow)
	if i < 0 {
		return data, false, nil
	}
	return render(settings, perms, slices.Delete(allow, i, i+1))
}

func render(settings, perms object, allow []json.RawMessage) ([]byte, bool, error) {
	if len(allow) == 0 {
		perms = perms.del("allow")
	} else {
		raw, err := json.Marshal(allow)
		if err != nil {
			return nil, false, err
		}
		perms = perms.set("allow", raw)
	}
	if len(perms) == 0 {
		settings = settings.del("permissions")
	} else {
		raw, err := perms.marshal()
		if err != nil {
			return nil, false, err
		}
		settings = settings.set("permissions", raw)
	}
	raw, err := settings.marshal()
	if err != nil {
		return nil, false, err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, raw, "", "  "); err != nil {
		return nil, false, err
	}
	b.WriteByte('\n')
	return b.Bytes(), true, nil
}

// apply updates path atomically and keeps one backup of the original. It writes
// through symlinks, so settings managed by a dotfiles repo stay linked.
func apply(path string, change func([]byte) ([]byte, bool, error)) (bool, error) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o600)
	data, err := os.ReadFile(path)
	exists := err == nil
	switch {
	case errors.Is(err, os.ErrNotExist):
		data = []byte("{}")
	case err != nil:
		return false, err
	default:
		fi, err := os.Stat(path)
		if err != nil {
			return false, err
		}
		mode = fi.Mode().Perm()
	}

	out, changed, err := change(data)
	if err != nil || !changed {
		return false, err
	}

	if exists {
		backup := path + ".decision-log-backup"
		if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(backup, data, mode); err != nil {
				return false, err
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	return true, os.Rename(tmp.Name(), path)
}

func main() {
	if len(os.Args) != 3 || (os.Args[1] != "add" && os.Args[1] != "remove") {
		fmt.Fprintln(os.Stderr, "usage: autosetup add|remove <settings.json>")
		os.Exit(2)
	}
	change := addRule
	if os.Args[1] == "remove" {
		change = removeRule
	}
	changed, err := apply(os.Args[2], change)
	if err != nil {
		fmt.Fprintf(os.Stderr, "autosetup: file left unchanged: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("changed:", changed)
}
