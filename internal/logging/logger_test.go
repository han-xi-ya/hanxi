package logging_test

import (
	"strings"
	"testing"

	"hubkit/internal/logging"
)

func TestRedactSensitive(t *testing.T) {
	cases := []struct {
		input    string
		contains string
		excludes string
	}{
		{
			input:    `frpc starting with token = "my_secret_token_12345" and user = admin`,
			contains: `token="******"`,
			excludes: `my_secret_token_12345`,
		},
		{
			input:    `header: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9`,
			contains: `Bearer ******`,
			excludes: `eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9`,
		},
		{
			input:    `normal log: server listening on port 8080`,
			contains: `server listening on port 8080`,
			excludes: `******`,
		},
	}

	for _, c := range cases {
		out := logging.Redact(c.input)
		if !strings.Contains(out, c.contains) {
			t.Errorf("expected redacted output to contain %q, got %q", c.contains, out)
		}
		if c.excludes != "" && strings.Contains(out, c.excludes) {
			t.Errorf("expected redacted output to NOT contain %q, got %q", c.excludes, out)
		}
	}
}
