package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

// runDiff compares two scan --json reports: call sites that appeared,
// disappeared, or changed cost, then the total monthly delta.
// Differences never move the exit code; only a bad input does.
func runDiff(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "overwater: diff expects two files: overwater diff OLD.json NEW.json (both from scan --json)")
		return ExitError
	}
	oldR, err := readScanJSON(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	newR, err := readScanJSON(args[1])
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	for _, line := range diffLines(oldR, newR) {
		fmt.Fprintln(stdout, line)
	}
	oldTotal, newTotal := total(oldR), total(newR)
	delta, sign := newTotal-oldTotal, "+"
	if delta < 0 {
		sign, delta = "-", -delta
	}
	fmt.Fprintf(stdout, "total: ~$%d/mo -> ~$%d/mo (%s$%d/mo)\n", oldTotal, newTotal, sign, delta)
	return ExitClean
}

// scanReport is the subset of scan --json output the diff reads. Every
// scan --json carries a catalog version, so its absence means the file
// is some other JSON entirely.
type scanReport struct {
	CatalogVersion string        `json:"catalog_version"`
	Findings       []scanFinding `json:"findings"`
}

type scanFinding struct {
	Rule           string `json:"rule"`
	File           string `json:"file"`
	CandidateModel string `json:"candidate_model"`
	MonthlyUSD     int    `json:"monthly_usd"`
}

func readScanJSON(path string) (*scanReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r scanReport
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s is not scan --json output: %w", path, err)
	}
	if r.CatalogVersion == "" {
		return nil, fmt.Errorf("%s is not scan --json output: it has no catalog_version", path)
	}
	return &r, nil
}

// siteKey identifies a call site across scans. Line numbers drift, so
// the key is rule, file, and the nominated model.
type siteKey struct {
	rule, file, candidate string
}

func describeSite(k siteKey) string {
	if k.candidate == "" {
		return fmt.Sprintf("%s at %s", k.rule, k.file)
	}
	return fmt.Sprintf("%s at %s (candidate %s)", k.rule, k.file, k.candidate)
}

func groupByKey(r *scanReport) map[siteKey][]int {
	m := map[siteKey][]int{}
	for _, f := range r.Findings {
		k := siteKey{f.Rule, f.File, f.CandidateModel}
		m[k] = append(m[k], f.MonthlyUSD)
	}
	for _, costs := range m {
		sort.Ints(costs)
	}
	return m
}

// cancelCommon removes the multiset intersection from both sorted cost
// lists; what remains is what actually moved.
func cancelCommon(o, n []int) ([]int, []int) {
	var ro, rn []int
	i, j := 0, 0
	for i < len(o) && j < len(n) {
		switch {
		case o[i] == n[j]:
			i++
			j++
		case o[i] < n[j]:
			ro = append(ro, o[i])
			i++
		default:
			rn = append(rn, n[j])
			j++
		}
	}
	ro = append(ro, o[i:]...)
	rn = append(rn, n[j:]...)
	return ro, rn
}

// diffLines renders one line per change, keys sorted by file, rule, and
// candidate for determinism. Leftover costs pair up as cost changes;
// the unpaired remainder appeared or disappeared.
func diffLines(oldR, newR *scanReport) []string {
	oldM, newM := groupByKey(oldR), groupByKey(newR)
	seen := map[siteKey]bool{}
	var keys []siteKey
	for k := range oldM {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range newM {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.file != b.file {
			return a.file < b.file
		}
		if a.rule != b.rule {
			return a.rule < b.rule
		}
		return a.candidate < b.candidate
	})
	var lines []string
	for _, k := range keys {
		o, n := cancelCommon(oldM[k], newM[k])
		paired := len(o)
		if len(n) < paired {
			paired = len(n)
		}
		for i := 0; i < paired; i++ {
			lines = append(lines, fmt.Sprintf("cost: %s ~$%d/mo -> ~$%d/mo", describeSite(k), o[i], n[i]))
		}
		for _, usd := range o[paired:] {
			lines = append(lines, fmt.Sprintf("disappeared: %s ~$%d/mo", describeSite(k), usd))
		}
		for _, usd := range n[paired:] {
			lines = append(lines, fmt.Sprintf("appeared: %s ~$%d/mo", describeSite(k), usd))
		}
	}
	return lines
}

func total(r *scanReport) int {
	sum := 0
	for _, f := range r.Findings {
		sum += f.MonthlyUSD
	}
	return sum
}
