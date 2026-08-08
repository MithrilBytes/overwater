package scan

import (
	"strings"
	"testing"
)

// A prompt that rules a task out must not score as if it asked for it.
func TestNegatedPhrasesDoNotScore(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   bool
	}{
		{"plain", "Reply to the customer warmly.", true},
		{"never", "Never reply to the customer.", false},
		{"do not", "Do not reply to the customer.", false},
		{"contraction", "Don't reply to the customer.", false},
		{"avoid", "Avoid reply to the customer entirely.", false},
		// A negation binds to its own clause, not to the next one.
		{"previous sentence", "Never promise refunds. Reply to the customer warmly.", true},
		{"previous clause", "Avoid small talk; reply to the customer warmly.", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := saysAny(strings.ToLower(tt.prompt), []string{"reply to the customer"})
			if got != tt.want {
				t.Errorf("saysAny(%q) = %v, want %v", tt.prompt, got, tt.want)
			}
		})
	}
}
