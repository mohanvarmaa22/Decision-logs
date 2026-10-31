package secrets

import (
	"strings"
	"testing"
)

func TestRedactsSecrets(t *testing.T) {
	cases := map[string]string{
		"aws":         "key AKIAIOSFODNN7EXAMPLE in config",
		"github":      "token ghp_" + strings.Repeat("a1B2", 9),
		"anthropic":   "uses sk-ant-api03-abcdefghijklmnopqrstuvwxyz",
		"openai":      "uses sk-proj-abcdefghijklmnopqrstuvwx",
		"jwt":         "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		"private key": "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----",
		"env":         "set DB_PASSWORD=hunter2 in prod",
		"entropy":     "the value 9fK2xQ7mZp4LwR8tY3vN6bH1cJ5sD0aE",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			out, changed := Redact(in)
			if !changed || !strings.Contains(out, Redacted) {
				t.Fatalf("not redacted: %q", out)
			}
		})
	}
}

func TestEnvKeepsName(t *testing.T) {
	out, _ := Redact("API_KEY=abc123")
	if out != "API_KEY="+Redacted {
		t.Fatalf("got %q", out)
	}
}

func TestLeavesOrdinaryTextAlone(t *testing.T) {
	for _, in := range []string{
		"Use Redis for session cache",
		"Multiple app servers; in-memory cache isn't shared",
		"src/internal/store/store_test.go",
		"internal/session/session_manager_impl.go",
		"Sticky sessions (breaks autoscaling)",
		"TOKEN_TTL is configurable",
		"commit 3f2a9c1e",
	} {
		if out, changed := Redact(in); changed {
			t.Errorf("false positive: %q -> %q", in, out)
		}
	}
}
