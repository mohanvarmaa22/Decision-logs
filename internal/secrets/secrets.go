// Package secrets replaces likely credentials in note text with [REDACTED].
// It catches common patterns, not everything.
package secrets

import (
	"math"
	"regexp"
	"strings"
)

const Redacted = "[REDACTED]"

var patterns = []*regexp.Regexp{
	// Private key blocks, including an unterminated header.
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----(?s:.*?)(-----END [A-Z ]*PRIVATE KEY-----|$)`),
	// AWS access key IDs.
	regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`),
	// GitHub tokens.
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{40,}\b`),
	// Anthropic and OpenAI-style keys.
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bsk-(proj-)?[A-Za-z0-9_-]{20,}`),
	// Slack tokens.
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`),
	// JWTs.
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
}

// .env-style assignments: the value is redacted, the name is kept so the note
// still reads sensibly.
var assignment = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:SECRET|TOKEN|PASSWORD|PASSWD|API_?KEY|PRIVATE_?KEY|ACCESS_?KEY)[A-Z0-9_]*)\s*[:=]\s*("[^"]*"|'[^']*'|\S+)`)

// Candidates for the entropy check: long runs of key-like characters.
var candidate = regexp.MustCompile(`[A-Za-z0-9+/=_-]{24,}`)

// Redact returns s with secrets replaced, and whether anything was replaced.
func Redact(s string) (string, bool) {
	out := s
	for _, p := range patterns {
		out = p.ReplaceAllString(out, Redacted)
	}
	out = assignment.ReplaceAllStringFunc(out, func(m string) string {
		sub := assignment.FindStringSubmatch(m)
		if sub[2] == Redacted {
			return m
		}
		return sub[1] + "=" + Redacted
	})
	out = candidate.ReplaceAllStringFunc(out, func(m string) string {
		if looksRandom(m) {
			return Redacted
		}
		return m
	})
	return out, out != s
}

// looksRandom flags high-entropy strings that mix letters and digits. Paths
// and ordinary identifiers stay below the threshold or lack the mix.
func looksRandom(s string) bool {
	if strings.Contains(s, Redacted) {
		return false
	}
	var lower, upper, digit bool
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		}
	}
	if !digit || !(lower || upper) {
		return false
	}
	return entropy(s) >= 4.0
}

// entropy is Shannon entropy in bits per character.
func entropy(s string) float64 {
	counts := map[rune]int{}
	for _, r := range s {
		counts[r]++
	}
	n := float64(len([]rune(s)))
	var h float64
	for _, c := range counts {
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}
