package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

type volumeReport struct {
	CallsPerMonth int `json:"calls_per_month"`
	Findings      []struct {
		File         string `json:"file"`
		MonthlyUSD   int    `json:"monthly_usd"`
		Volume       int    `json:"volume"`
		VolumeSource string `json:"volume_source"`
	} `json:"findings"`
}

func volumesScan(t *testing.T, args ...string) volumeReport {
	t.Helper()
	code, stdout, stderr := run(t, append([]string{"scan", "-json"}, args...)...)
	if code != ExitClean {
		t.Fatalf("scan exit = %d, stderr = %q", code, stderr)
	}
	var report volumeReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, stdout)
	}
	return report
}

// A volumes file moves the dollars and the wording together.
func TestScanWithVolumesFile(t *testing.T) {
	repo := repoWith(t, "")
	base := volumesScan(t, repo)
	if len(base.Findings) != 1 || base.Findings[0].VolumeSource != "estimate" {
		t.Fatalf("control run: %+v", base.Findings)
	}
	vols := writeFile(t, t.TempDir(), "volumes.json", `{"sites": {"legacy.py:1": 30000}}`)
	got := volumesScan(t, "-volumes", vols, repo)
	if len(got.Findings) != 1 {
		t.Fatalf("got %+v, want one finding", got.Findings)
	}
	f := got.Findings[0]
	if f.Volume != 30000 || f.VolumeSource != "measured" {
		t.Errorf("volume = %d from %q, want 30000 measured", f.Volume, f.VolumeSource)
	}
	if want := base.Findings[0].MonthlyUSD * 3; f.MonthlyUSD != want {
		t.Errorf("monthly_usd = %d, want %d at three times the calls", f.MonthlyUSD, want)
	}
	code, stdout, _ := run(t, "scan", "-volumes", vols, repo)
	if code != ExitClean || !strings.Contains(stdout, "at measured volume") {
		t.Errorf("text verdict = %q, want it to say the volume was measured", stdout)
	}
	if strings.Contains(stdout, "at estimated volume") {
		t.Errorf("text verdict = %q, still calls a measured number an estimate", stdout)
	}
}

// A model key covers the sites the file does not name one by one.
func TestScanVolumesModelKey(t *testing.T) {
	repo := repoWith(t, "")
	vols := writeFile(t, t.TempDir(), "volumes.json", `{"models": {"text-davinci-003": 40000}}`)
	got := volumesScan(t, "-volumes", vols, repo)
	if len(got.Findings) != 1 || got.Findings[0].Volume != 40000 {
		t.Fatalf("got %+v, want the model key's 40000", got.Findings)
	}
}

func TestScanVolumesReportsUnknownKeys(t *testing.T) {
	repo := repoWith(t, "")
	vols := writeFile(t, t.TempDir(), "volumes.json",
		`{"sites": {"gone.py:9": 1}, "models": {"text-davinci-003": 2, "gpt-4o": 3}}`)
	code, _, stderr := run(t, "scan", "-volumes", vols, repo)
	if code != ExitClean {
		t.Fatalf("exit = %d, want a clean scan; stderr = %q", code, stderr)
	}
	for _, want := range []string{"no call site at gone.py:9", "no call site uses model gpt-4o"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to name %q", stderr, want)
		}
	}
	if strings.Contains(stderr, "text-davinci-003") {
		t.Errorf("stderr = %q, want the key that matched to stay quiet", stderr)
	}
}

func TestScanVolumesMalformedExitsTwo(t *testing.T) {
	repo := repoWith(t, "")
	dir := t.TempDir()
	cases := map[string]string{
		"not json":      "volumes: lots\n",
		"unknown field": `{"site": {"a.py:1": 5}}`,
		"bad site key":  `{"sites": {"a.py": 5}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			vols := writeFile(t, dir, "volumes.json", body)
			code, _, stderr := run(t, "scan", "-volumes", vols, repo)
			if code != ExitError {
				t.Fatalf("exit = %d, want %d for a malformed volumes file", code, ExitError)
			}
			if !strings.Contains(stderr, "volumes.json") {
				t.Errorf("stderr = %q, want it to name the file", stderr)
			}
		})
	}
	code, _, _ := run(t, "scan", "-volumes", filepath.Join(dir, "absent.json"), repo)
	if code != ExitError {
		t.Errorf("exit = %d, want %d for a missing volumes file", code, ExitError)
	}
}

// The budget check prices what the report prices: triple the volume,
// triple the number the budget compares.
func TestBudgetAgreesWithMeasuredVolumes(t *testing.T) {
	base := volumesScan(t, repoWith(t, ""))
	if len(base.Findings) != 1 {
		t.Fatalf("control run: %+v", base.Findings)
	}
	monthly := base.Findings[0].MonthlyUSD
	config := "budget_monthly_usd: " + strconv.Itoa(monthly*2) + "\n"
	vols := writeFile(t, t.TempDir(), "volumes.json", `{"sites": {"legacy.py:1": 30000}}`)
	code, _, stderr := run(t, "scan", "-volumes", vols, repoWith(t, config))
	if code != ExitFindings {
		t.Fatalf("exit = %d, want %d once measured volumes blow the budget; stderr = %q", code, ExitFindings, stderr)
	}
	if !strings.Contains(stderr, "exceeds budget_monthly_usd") {
		t.Errorf("stderr = %q, want the budget line", stderr)
	}
	// Same budget, no volumes file: the estimate stays under it.
	code, _, stderr = run(t, "scan", repoWith(t, config))
	if code != ExitClean || strings.Contains(stderr, "budget") {
		t.Errorf("without the volumes file: code %d, stderr %q, want a quiet clean exit", code, stderr)
	}
}

const messyCSV = `Date,  Model Name ,Region,N_Requests
2026-07-01,gpt-5.1,us,1200
2026-07-02,gpt-5.1,eu,"3,400"
2026-07-02,mystery-9000,eu,7
`

func TestVolumesImportCSV(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "usage.csv", messyCSV)
	out := filepath.Join(dir, "volumes.json")
	code, _, stderr := run(t, "volumes", "import", "-o", out, src)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{`model column "Model Name"`, `count column "N_Requests"`, "3 rows"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to state %q", stderr, want)
		}
	}
	if !strings.Contains(stderr, "mystery-9000") {
		t.Errorf("stderr = %q, want the model the catalog does not know reported", stderr)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Models map[string]int `json:"models"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("bad volumes file: %v\n%s", err, raw)
	}
	if got.Models["gpt-5.1"] != 4600 {
		t.Errorf("gpt-5.1 = %d, want the two rows summed to 4600", got.Models["gpt-5.1"])
	}
	if got.Models["mystery-9000"] != 7 {
		t.Errorf("mystery-9000 = %d, want the row kept, not dropped", got.Models["mystery-9000"])
	}
}

func TestVolumesImportJSON(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "usage.json", `[
  {"model_id": "gpt-5.1", "num_requests": 1200},
  {"model_id": "gpt-5.1", "num_requests": "800"},
  {"model_id": "gpt-4o", "num_requests": 5}
]`)
	code, stdout, stderr := run(t, "volumes", "import", "-o", "-", src)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{`model field "model_id"`, `count field "num_requests"`, "3 records"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to state %q", stderr, want)
		}
	}
	if !strings.Contains(stdout, `"gpt-5.1": 2000`) {
		t.Errorf("volumes = %s, want gpt-5.1 summed to 2000", stdout)
	}
}

// The imported file is one the scan reads back without complaint.
func TestVolumesImportFeedsScan(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "usage.csv", "model,requests\ntext-davinci-003,50000\n")
	out := filepath.Join(dir, "volumes.json")
	if code, _, stderr := run(t, "volumes", "import", "-o", out, src); code != ExitClean {
		t.Fatalf("import exit = %d, stderr = %q", code, stderr)
	}
	got := volumesScan(t, "-volumes", out, repoWith(t, ""))
	if len(got.Findings) != 1 || got.Findings[0].Volume != 50000 {
		t.Fatalf("got %+v, want the imported 50000", got.Findings)
	}
}

func TestVolumesImportRejects(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"no model column":    "date,requests\n2026-07-01,5\n",
		"no count column":    "model,region\ngpt-5.1,us\n",
		"header only":        "model,requests\n",
		"count not a number": "model,requests\ngpt-5.1,many\n",
		"negative count":     "model,requests\ngpt-5.1,-4\n",
		"empty json array":   "[]",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			src := writeFile(t, dir, "usage.csv", body)
			code, _, stderr := run(t, "volumes", "import", "-o", "-", src)
			if code != ExitError {
				t.Fatalf("exit = %d, want %d; stderr = %q", code, ExitError, stderr)
			}
		})
	}
	if code, _, _ := run(t, "volumes"); code != ExitError {
		t.Errorf("bare volumes exit = %d, want %d", code, ExitError)
	}
	if code, _, _ := run(t, "volumes", "export"); code != ExitError {
		t.Errorf("unknown subcommand exit = %d, want %d", code, ExitError)
	}
}
