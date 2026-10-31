// Command transcript is a throwaway probe for prototype test 2: can Claude Code's
// chat logs be read reliably enough for the backstop and for backfill? It prints
// measurements only, unless --samples asks for short redacted excerpts.
//
// The log format is undocumented, so unknown line types and fields are ignored.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/decision-log/decision-log/internal/secrets"
)

type line struct {
	Type          string          `json:"type"`
	UUID          string          `json:"uuid"`
	ParentUUID    string          `json:"parentUuid"`
	IsSidechain   bool            `json:"isSidechain"`
	IsMeta        bool            `json:"isMeta"`
	IsCompact     bool            `json:"isCompactSummary"`
	TranscriptOnl bool            `json:"isVisibleInTranscriptOnly"`
	Message       json.RawMessage `json:"message"`
}

type message struct {
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// turn is one real user prompt and everything the assistant did until the next one.
type turn struct {
	Text    strings.Builder // assistant-visible text only, never thinking
	Bytes   int             // raw log bytes the turn spans
	NoteRun bool            // a tool call ran `decision-log add`
}

type stats struct {
	lines, malformed, duplicates, unreadable, sidechain, branchPoints int
	longestLine                                                       int
	turns                                                             []*turn
}

// parse streams one log. bufio.Reader, not bufio.Scanner: log lines exceed
// Scanner's default 64 KB limit.
func parse(r io.Reader) (*stats, error) {
	s := &stats{}
	br := bufio.NewReader(r)
	seen := map[string]bool{}
	children := map[string]int{}
	var cur *turn
	for {
		raw, err := br.ReadBytes('\n')
		if len(raw) > 0 {
			s.lines++
			s.longestLine = max(s.longestLine, len(raw))
			cur = s.addLine(raw, seen, children, cur)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return s, err
		}
	}
	for _, n := range children {
		if n > 1 {
			s.branchPoints++
		}
	}
	return s, nil
}

func (s *stats) addLine(raw []byte, seen map[string]bool, children map[string]int, cur *turn) *turn {
	var l line
	if err := json.Unmarshal(raw, &l); err != nil {
		s.malformed++
		return cur
	}
	if cur != nil {
		cur.Bytes += len(raw)
	}
	if l.Type != "user" && l.Type != "assistant" {
		return cur
	}
	if l.IsSidechain {
		s.sidechain++
		return cur
	}
	if l.UUID != "" {
		if seen[l.UUID] {
			s.duplicates++
			return cur
		}
		seen[l.UUID] = true
	}
	if l.ParentUUID != "" {
		children[l.ParentUUID]++
	}
	text, blocks, ok := content(l.Message)
	if !ok {
		s.unreadable++
		return cur
	}
	if l.Type == "user" {
		if isPrompt(l, text, blocks) {
			cur = &turn{Bytes: len(raw)}
			s.turns = append(s.turns, cur)
		}
		return cur
	}
	if cur == nil {
		return cur
	}
	cur.Text.WriteString(text)
	for _, b := range blocks {
		switch {
		case b.Type == "text":
			cur.Text.WriteString(b.Text + "\n")
		case b.Type == "tool_use" && b.Name == "Bash" && strings.Contains(string(b.Input), "decision-log add"):
			cur.NoteRun = true
		}
	}
	return cur
}

// content accepts both shapes the log uses: a plain string or a list of blocks.
func content(raw json.RawMessage) (string, []block, bool) {
	var m message
	if json.Unmarshal(raw, &m) != nil || len(m.Content) == 0 {
		return "", nil, false
	}
	var str string
	if json.Unmarshal(m.Content, &str) == nil {
		return str, nil, true
	}
	var blocks []block
	if json.Unmarshal(m.Content, &blocks) == nil {
		return "", blocks, true
	}
	return "", nil, false
}

// isPrompt separates what a person typed from tool results and injected text,
// which share the "user" line type.
func isPrompt(l line, text string, blocks []block) bool {
	if l.IsMeta || l.IsCompact || l.TranscriptOnl {
		return false
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return false
		}
		if b.Type == "text" && text == "" {
			text = b.Text
		}
	}
	t := strings.TrimSpace(text)
	return t != "" && !strings.HasPrefix(t, "<command-") && !strings.HasPrefix(t, "<local-command") &&
		!strings.HasPrefix(t, "<task-notification") && !strings.HasPrefix(t, "[Request interrupted")
}

var wordSets = []struct {
	name string
	re   *regexp.Regexp
}{
	{"broad", regexp.MustCompile(`(?i)\b(decid\w+|chose|chosen|choos\w+|going with|went with|opted?|instead of|rather than|trade-?offs?|recommend\w*|prefer\w*|better than)\b`)},
	{"strict", regexp.MustCompile(`(?i)\b(decided|chose|going with|went with|opted for|(use|using|keep|picked?) \S+(?: \S+){0,3}? (instead of|rather than|over))\b`)},
}

func main() {
	samples := flag.Int("samples", 0, "print N short redacted excerpts per word set (they contain chat text; never commit them)")
	days := flag.Int("days", 30, "only logs modified in the last N days")
	flag.Parse()

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	files, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "*.jsonl"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cutoff := time.Now().AddDate(0, 0, -*days)
	var total stats
	var slowest time.Duration
	var biggest int64
	used := 0
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil || fi.ModTime().Before(cutoff) {
			continue
		}
		fh, err := os.Open(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		start := time.Now()
		s, err := parse(fh)
		took := time.Since(start)
		fh.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, f, err)
		}
		used++
		if fi.Size() > biggest {
			biggest, slowest = fi.Size(), took
		}
		total.lines += s.lines
		total.malformed += s.malformed
		total.duplicates += s.duplicates
		total.unreadable += s.unreadable
		total.sidechain += s.sidechain
		total.branchPoints += s.branchPoints
		total.longestLine = max(total.longestLine, s.longestLine)
		total.turns = append(total.turns, s.turns...)
	}

	fmt.Printf("logs read:            %d\n", used)
	fmt.Printf("lines:                %d (malformed %d, unreadable content %d)\n", total.lines, total.malformed, total.unreadable)
	fmt.Printf("duplicate uuids:      %d\n", total.duplicates)
	fmt.Printf("sidechain lines:      %d (skipped)\n", total.sidechain)
	fmt.Printf("branch points:        %d (rewinds or forks, or parallel tool calls)\n", total.branchPoints)
	fmt.Printf("longest line:         %d KB\n", total.longestLine/1024)
	fmt.Printf("biggest log:          %.1f MB, parsed in %s\n", float64(biggest)/1e6, slowest.Round(time.Millisecond))
	fmt.Printf("user turns:           %d\n", len(total.turns))

	if n := len(total.turns); n > 0 {
		sizes := make([]int, n)
		for i, t := range total.turns {
			sizes[i] = t.Bytes
		}
		sort.Ints(sizes)
		fmt.Printf("turn size in log:     p50 %d KB, p95 %d KB, max %d KB\n", sizes[n/2]/1024, sizes[n*95/100]/1024, sizes[n-1]/1024)
		for _, w := range wordSets {
			hits := 0
			for _, t := range total.turns {
				if w.re.MatchString(t.Text.String()) {
					hits++
				}
			}
			fmt.Printf("%-7s word hits:      %d of %d turns (%.0f%%)\n", w.name, hits, n, 100*float64(hits)/float64(n))
			printSamples(total.turns, w.re, *samples)
		}
	}
}

func printSamples(turns []*turn, re *regexp.Regexp, n int) {
	if n == 0 {
		return
	}
	var hits []string
	for _, t := range turns {
		text := t.Text.String()
		if loc := re.FindStringIndex(text); loc != nil {
			from, to := max(0, loc[0]-70), min(len(text), loc[1]+70)
			hits = append(hits, strings.Join(strings.Fields(text[from:to]), " "))
		}
	}
	// Evenly spaced, so one long session doesn't fill the sample.
	step := max(1, len(hits)/n)
	for i := 0; i < len(hits) && i/step < n; i += step {
		out, _ := secrets.Redact(hits[i])
		fmt.Printf("    - %s\n", out)
	}
}
