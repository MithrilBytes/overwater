package scan

import (
	"path"
	"regexp"
	"strings"
)

// Calls that spend tokens without naming a model.
//
// Everything else in this package is built on a model reference: a Site
// is one, layer 3 reads shape around it, layer 4 classifies it, and the
// rules engine prices it. Two real shapes spend money and never produce
// that reference.
//
//	fetch(`${cfg.apiBaseUrl}/chat/completions`, {body: JSON.stringify({
//	  model: cfg.model,
//	})})
//
//	claude --print --output-format json --system-prompt "$system"
//	run_one claude-opus --agent claude --model opus
//
// The first names its model through a variable. The second names it as
// an agent CLI alias, or not at all, and lets the CLI pick.
//
// Neither can be priced, and pretending otherwise would be worse than
// silence: a price needs a catalog entry, and "opus" is not one. What
// they can do is mark where the scanner's knowledge stops, so a repo
// that spends money through one of these does not read as a clean bill
// of health. That is the same contract as an unrecognised model id, and
// it reaches the user the same way.

// UnpricedCall is a call site that spends tokens with no model this
// scanner can resolve. It carries no price, no archetype and no rule.
type UnpricedCall struct {
	File     string // slash separated, relative to the repo root
	Line     int
	Kind     string // endpoint or agent-cli
	Evidence string // the trimmed source line, for the reader to judge
}

// Provider inference paths. A URL is the one part of an HTTP call that
// survives every client library, template literal and string builder, so
// it is what gets matched rather than the call expression around it.
var endpointRE = regexp.MustCompile(
	`(?i)(/v1)?/chat/completions|/v1/messages\b|:generateContent\b|` +
		`/v1/complete\b|(?i)/api/(generate|chat)\b|/v1/embeddings\b|/v1/responses\b`)

// Agent CLIs that bill against a subscription or API key. A bare
// "claude" or "codex" is far too common to match on its own, so each
// needs a flag that only the non interactive form takes.
var agentCLIRE = regexp.MustCompile(
	`(?i)\b(claude|codex|gemini|aider|cursor-agent|amp|goose)\b[^|;&\n]*?` +
		`\s(--print\b|-p\b|exec\b|--model\b|--agent\b|--message\b|--prompt\b)`)

// Shells and the places shell lives. An agent CLI invocation in a Go or
// TypeScript file is usually a string being built for exec, which the
// same pattern catches inside the string.
var execExts = map[string]bool{
	".sh": true, ".bash": true, ".zsh": true, ".fish": true,
	".yaml": true, ".yml": true, ".mk": true, "": true,
}

// findUnpricedCalls reports calls that spend tokens without naming a
// model this scanner can resolve. Lines that already carry a model
// reference are skipped: those have a site, and a site is the better
// answer.
func findUnpricedCalls(relPath, masked string, priced map[int]bool) []UnpricedCall {
	var out []UnpricedCall
	ext := strings.ToLower(path.Ext(relPath))
	execish := execExts[ext] || strings.Contains(relPath, "Makefile") ||
		strings.Contains(relPath, "Dockerfile")

	for i, line := range strings.Split(masked, "\n") {
		lineNo := i + 1
		if priced[lineNo] || strings.TrimSpace(line) == "" {
			continue
		}
		kind := ""
		switch {
		case endpointRE.MatchString(line):
			kind = "endpoint"
		case execish && agentCLIRE.MatchString(line):
			kind = "agent-cli"
		default:
			continue
		}
		trimmed := truncateRunes(strings.TrimSpace(line), unpricedEvidenceMax)
		out = append(out, UnpricedCall{
			File: relPath, Line: lineNo, Kind: kind, Evidence: trimmed,
		})
	}
	return out
}

// Long enough to recognise the call, short enough to stay one line.
const unpricedEvidenceMax = 70

// truncateRunes cuts on a character boundary, counting characters
// rather than bytes.
//
// Slicing a string at a byte offset splits whatever multi byte
// character straddles it, and the half that survives is not valid
// UTF-8. This line is printed on stderr, so a repository with an Arabic
// string in a fetch call emitted bytes that no reader could decode: it
// crashed a Python harness mid sweep rather than showing up as odd
// looking output.
func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s // cannot exceed max runes when it does not exceed max bytes
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
