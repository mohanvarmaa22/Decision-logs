package main

import (
	"fmt"
	"strings"
	"testing"
)

func logOf(lines ...string) *strings.Reader {
	return strings.NewReader(strings.Join(lines, "\n") + "\n")
}

func TestParseBuildsTurns(t *testing.T) {
	s, err := parse(logOf(
		`{"type":"user","uuid":"1","message":{"content":"Redis or memcached?"}}`,
		`{"type":"assistant","uuid":"2","message":{"content":[{"type":"thinking","thinking":"hidden"}]}}`,
		`{"type":"assistant","uuid":"3","message":{"content":[{"type":"text","text":"Going with Redis."}]}}`,
		`{"type":"assistant","uuid":"4","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"decision-log add --decision x"}}]}}`,
		`{"type":"user","uuid":"5","message":{"content":[{"type":"tool_result","content":"saved"}]}}`,
		`{"type":"attachment","uuid":"6"}`,
		`{"type":"user","uuid":"7","message":{"content":[{"type":"text","text":"thanks"}]}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.turns) != 2 {
		t.Fatalf("want 2 turns, got %d", len(s.turns))
	}
	first := s.turns[0]
	if !strings.Contains(first.Text.String(), "Going with Redis") || strings.Contains(first.Text.String(), "hidden") {
		t.Errorf("bad turn text: %q", first.Text.String())
	}
	if !first.NoteRun || s.turns[1].NoteRun {
		t.Error("note run attached to the wrong turn")
	}
}

func TestParseSkipsNonPrompts(t *testing.T) {
	s, _ := parse(logOf(
		`{"type":"user","uuid":"1","isMeta":true,"message":{"content":"meta"}}`,
		`{"type":"user","uuid":"2","isCompactSummary":true,"message":{"content":"summary"}}`,
		`{"type":"user","uuid":"3","message":{"content":"<command-name>/init</command-name>"}}`,
		`{"type":"user","uuid":"4","isSidechain":true,"message":{"content":"subagent prompt"}}`,
		`{"type":"assistant","uuid":"5","message":{"content":[{"type":"text","text":"orphan"}]}}`,
	))
	if len(s.turns) != 0 {
		t.Fatalf("want no turns, got %d", len(s.turns))
	}
	if s.sidechain != 1 {
		t.Errorf("sidechain = %d", s.sidechain)
	}
}

func TestParseSurvivesBadInput(t *testing.T) {
	s, err := parse(logOf(
		`not json`,
		`{"type":"user","uuid":"1","message":{"content":"hi"}}`,
		`{"type":"user","uuid":"1","message":{"content":"hi"}}`,
		`{"type":"assistant","uuid":"2","message":{"content":42}}`,
		`{"type":"brand-new-type","whatever":true}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if s.malformed != 1 || s.duplicates != 1 || s.unreadable != 1 || len(s.turns) != 1 {
		t.Fatalf("got %+v", s)
	}
}

func TestParseHandlesLinesOverScannerLimit(t *testing.T) {
	huge := strings.Repeat("x", 5<<20)
	s, err := parse(logOf(fmt.Sprintf(`{"type":"user","uuid":"1","message":{"content":"%s"}}`, huge)))
	if err != nil || len(s.turns) != 1 {
		t.Fatalf("err=%v turns=%d", err, len(s.turns))
	}
}

func TestParseLastLineWithoutNewline(t *testing.T) {
	s, _ := parse(strings.NewReader(`{"type":"user","uuid":"1","message":{"content":"hi"}}`))
	if len(s.turns) != 1 {
		t.Fatalf("turns = %d", len(s.turns))
	}
}

func TestWordSets(t *testing.T) {
	broad, strict := wordSets[0].re, wordSets[1].re
	if !broad.MatchString("I recommend Redis") || strict.MatchString("I recommend Redis") {
		t.Error("recommend belongs only in the broad set")
	}
	if !strict.MatchString("We decided on Redis") || !strict.MatchString("use Redis instead of memcached") {
		t.Error("strict set missed a real decision")
	}
	if strict.MatchString("Fixed the typo") {
		t.Error("strict set matched a non-decision")
	}
}
