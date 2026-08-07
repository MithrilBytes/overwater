package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/rules"
)

// volumesFile is a parsed --volumes file and the path it came from, so
// stderr notes can name it.
type volumesFile struct {
	path    string
	volumes *rules.Volumes
}

// loadVolumes reads a measured traffic file. Every failure is an
// operational error: a run that meant to price on real numbers must not
// quietly fall back to the estimate.
func loadVolumes(path string) (*volumesFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	v, err := rules.ParseVolumes(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &volumesFile{path: path, volumes: v}, nil
}

// runVolumes routes the volumes subcommands. Only import exists so far.
func runVolumes(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printVolumesUsage(stderr)
		return ExitError
	}
	switch args[0] {
	case "import":
		return runVolumesImport(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printVolumesUsage(stdout)
		return ExitClean
	}
	fmt.Fprintf(stderr, "overwater volumes: unknown subcommand %q\n", args[0])
	printVolumesUsage(stderr)
	return ExitError
}

func printVolumesUsage(w io.Writer) {
	fmt.Fprint(w, "Turn a provider usage export into a volumes file for scan --volumes.\n\n")
	fmt.Fprint(w, "Usage:\n\n  overwater volumes import [-o FILE] USAGE-EXPORT\n\n")
	fmt.Fprint(w, "The export is a local CSV or JSON file naming models and request\ncounts. No provider API is called.\n\n")
}

// runVolumesImport turns a provider usage export into a volumes file
// keyed by model. It reads the named local file and nothing else; there
// is no provider API call anywhere in this path.
func runVolumesImport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("volumes import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "volumes.json", `write the volumes file here ("-" for stdout)`)
	if err := fs.Parse(args); err != nil {
		return ExitError
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "overwater: volumes import expects one usage export file")
		return ExitError
	}
	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	counts, note, err := importUsage(raw)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %s: %v\n", fs.Arg(0), err)
		return ExitError
	}
	fmt.Fprintf(stderr, "volumes import: %s\n", note)
	reportUnknownModels(counts, stderr)
	data, err := (&rules.Volumes{Models: counts}).JSON()
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	if *out == "-" {
		stdout.Write(data)
		return ExitClean
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(stderr, "wrote %s\n", *out)
	return ExitClean
}

// reportUnknownModels names imported models the catalog does not carry.
// They stay in the file: an export row is real traffic whatever the
// catalog knows, and dropping it silently would understate the total.
func reportUnknownModels(counts map[string]int, stderr io.Writer) {
	cat, _, err := catalog.Effective()
	if err != nil {
		return
	}
	names := cat.Names()
	var unknown []string
	for model := range counts {
		if names[model] == nil {
			unknown = append(unknown, model)
		}
	}
	if len(unknown) == 0 {
		return
	}
	sort.Strings(unknown)
	fmt.Fprintf(stderr, "volumes import: not in the catalog, kept as written: %s\n", strings.Join(unknown, ", "))
}

// Column names a usage export may use, in preference order. Matching
// folds case and drops everything but letters and digits, so "Model
// Name", "model_name", and "modelName" are one name.
var (
	modelColumns = []string{"model", "modelid", "modelname", "modelversion"}
	countColumns = []string{
		"requests", "nrequests", "numrequests", "requestcount",
		"calls", "ncalls", "numcalls", "callcount", "count",
	}
)

func foldColumn(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// pickColumn finds the first wanted column present in the header and
// returns its name as written and its position. Duplicate spellings of
// one column resolve to the leftmost.
func pickColumn(header []string, want []string) (string, int, bool) {
	at := make(map[string]int, len(header))
	for i, name := range header {
		key := foldColumn(name)
		if _, taken := at[key]; !taken {
			at[key] = i
		}
	}
	for _, w := range want {
		if i, ok := at[w]; ok {
			return header[i], i, true
		}
	}
	return "", 0, false
}

// importUsage reads a usage export in either supported shape and sums
// its rows by model. The note is the line the command prints: which
// columns it read and how many rows.
func importUsage(raw []byte) (map[string]int, string, error) {
	if isJSONArray(raw) {
		return importJSONUsage(raw)
	}
	return importCSVUsage(raw)
}

// isJSONArray detects the JSON shape by its first meaningful byte. A
// CSV header can hold anything but never opens with a bracket.
func isJSONArray(raw []byte) bool {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '['
}

func importCSVUsage(raw []byte) (map[string]int, string, error) {
	r := csv.NewReader(bytes.NewReader(raw))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, "", err
	}
	if len(rows) == 0 {
		return nil, "", fmt.Errorf("no header row")
	}
	header := rows[0]
	modelCol, modelAt, ok := pickColumn(header, modelColumns)
	if !ok {
		return nil, "", fmt.Errorf("no model column in header %s", strings.Join(header, ","))
	}
	countCol, countAt, ok := pickColumn(header, countColumns)
	if !ok {
		return nil, "", fmt.Errorf("no request count column in header %s", strings.Join(header, ","))
	}
	counts := map[string]int{}
	read := 0
	for i, row := range rows[1:] {
		line := i + 2
		if len(row) == 1 && strings.TrimSpace(row[0]) == "" {
			continue
		}
		if modelAt >= len(row) || countAt >= len(row) {
			return nil, "", fmt.Errorf("line %d has %d fields, too few for %s and %s", line, len(row), modelCol, countCol)
		}
		model := strings.TrimSpace(row[modelAt])
		if model == "" {
			return nil, "", fmt.Errorf("line %d has no model", line)
		}
		n, err := parseCount(row[countAt])
		if err != nil {
			return nil, "", fmt.Errorf("line %d: %w", line, err)
		}
		counts[model] += n
		read++
	}
	if read == 0 {
		return nil, "", fmt.Errorf("header only, no rows")
	}
	note := fmt.Sprintf("CSV, model column %q, count column %q, %d rows",
		strings.TrimSpace(modelCol), strings.TrimSpace(countCol), read)
	return counts, note, nil
}

func importJSONUsage(raw []byte) (map[string]int, string, error) {
	var records []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, "", err
	}
	if len(records) == 0 {
		return nil, "", fmt.Errorf("no records")
	}
	keys := make([]string, 0, len(records[0]))
	for k := range records[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	modelKey, _, ok := pickColumn(keys, modelColumns)
	if !ok {
		return nil, "", fmt.Errorf("no model field in %s", strings.Join(keys, ","))
	}
	countKey, _, ok := pickColumn(keys, countColumns)
	if !ok {
		return nil, "", fmt.Errorf("no request count field in %s", strings.Join(keys, ","))
	}
	counts := map[string]int{}
	for i, rec := range records {
		model, err := jsonString(rec[modelKey])
		if err != nil || model == "" {
			return nil, "", fmt.Errorf("record %d has no %s", i+1, modelKey)
		}
		n, err := parseCount(string(rec[countKey]))
		if err != nil {
			return nil, "", fmt.Errorf("record %d: %w", i+1, err)
		}
		counts[model] += n
	}
	return counts, fmt.Sprintf("JSON, model field %q, count field %q, %d records", modelKey, countKey, len(records)), nil
}

// jsonString reads a record field that should be a string, tolerating a
// number written as one.
func jsonString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("missing")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s), nil
	}
	return strings.Trim(string(raw), `"`), nil
}

// parseCount reads a request count. Exports write counts as integers,
// as quoted strings, and occasionally as floats; none of them may be
// negative.
func parseCount(raw string) (int, error) {
	text := strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), `"`))
	text = strings.ReplaceAll(text, ",", "")
	if text == "" {
		return 0, fmt.Errorf("empty request count")
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("request count %q is not a number", raw)
	}
	if f < 0 {
		return 0, fmt.Errorf("request count %q is negative", raw)
	}
	return int(math.Round(f)), nil
}
