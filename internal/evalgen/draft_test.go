package evalgen

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MithrilBytes/overwater/rules"
)

func TestDraftPrompts(t *testing.T) {
	long := "This prompt is comfortably longer than twenty characters."
	cases := []struct {
		name    string
		content string
		line    int
		want    []string
	}{
		{"no literals", "def f():\n    return 1\n", 1, nil},
		{"short literal is skipped", `x = "tiny"` + "\n", 1, nil},
		{
			"padding does not make a literal long enough",
			`x = "                    abc"` + "\n", 1, nil,
		},
		{
			"duplicates collapse",
			fmt.Sprintf("a = %q\nb = %q\n", long, long), 1, []string{long},
		},
		{
			"python triple quote yields the inner text",
			`PROMPT = """` + long + `"""` + "\n", 1, []string{long},
		},
		{
			"literal outside the window is ignored",
			strings.Repeat("\n", 100) + fmt.Sprintf("x = %q\n", long), 1, nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := draftPrompts(tc.content, tc.line)
			if len(got) != len(tc.want) {
				t.Fatalf("draftPrompts = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("prompt %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Twelve distinct literals cap at ten, with no duplicates.
func TestDraftPromptsCapsAtTen(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "p%d = \"Prompt number %02d padded out well past twenty characters.\"\n", i, i)
	}
	got := draftPrompts(b.String(), 6)
	if len(got) != draftMaxPrompts {
		t.Fatalf("drafted %d prompts, want the cap of %d", len(got), draftMaxPrompts)
	}
	seen := map[string]bool{}
	for _, p := range got {
		if seen[p] {
			t.Errorf("duplicate prompt %q", p)
		}
		seen[p] = true
	}
}

// One jsonl per script name; findings without a candidate, unreadable
// files, and literal free files are all skipped.
func TestDraftPromptSets(t *testing.T) {
	root := t.TempDir()
	outDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := "Summarize the ticket for the support dashboard, tersely."
	src := `PROMPT = """` + prompt + `"""` + "\ncall(model, PROMPT)\n"
	if err := os.WriteFile(filepath.Join(root, "app", "main.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bare.py"), []byte("x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := func(file string, candidate string) rules.Finding {
		return rules.Finding{RuleID: "tier-downgrade", File: file, Line: 2, Model: "big", CandidateModel: candidate}
	}
	written, err := DraftPromptSets(root, []rules.Finding{
		f("app/main.py", "small"),
		f("app/main.py", ""),     // no candidate, no script, no drafts
		f("missing.py", "small"), // unreadable file skipped silently
		f("bare.py", "small"),    // no literals, nothing to write
	}, outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 {
		t.Fatalf("written = %v, want exactly the one drafted set", written)
	}
	want := filepath.Join(outDir, "eval_tier-downgrade_app_main_py_2.prompts.jsonl")
	if written[0] != want {
		t.Errorf("path = %q, want %q", written[0], want)
	}
	file, err := os.Open(written[0])
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var prompts []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var row struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("bad JSONL line: %v", err)
		}
		prompts = append(prompts, row.Prompt)
	}
	if len(prompts) != 1 || prompts[0] != prompt {
		t.Errorf("prompts = %v, want the triple quoted literal", prompts)
	}
}
