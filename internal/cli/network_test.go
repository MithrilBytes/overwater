package cli

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MithrilBytes/overwater/catalog"
)

// countingTransport counts every request the binary tries to make. With
// deny set it refuses them; otherwise it serves the embedded catalog.
type countingTransport struct {
	calls int
	deny  bool
}

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	if t.deny {
		return nil, errors.New("network denied by test")
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(catalog.EmbeddedJSON())),
		Request:    req,
	}, nil
}

func swapTransport(t *testing.T, tr http.RoundTripper) {
	t.Helper()
	old := httpClient.Transport
	httpClient.Transport = tr
	t.Cleanup(func() { httpClient.Transport = old })
}

// The trust boundary, enforced: zero requests under --offline.
func TestScanOfflineMakesZeroRequests(t *testing.T) {
	tr := &countingTransport{deny: true}
	swapTransport(t, tr)
	code, _, stderr := runScanArgs(t, "-refresh", "-offline", fixturePath("clean-app"))
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if tr.calls != 0 {
		t.Fatalf("scanner made %d requests under --offline, want 0", tr.calls)
	}
	if !strings.Contains(stderr, "skipping catalog refresh") {
		t.Errorf("stderr = %q, want an offline note", stderr)
	}
}

// A plain scan makes no requests at all.
func TestPlainScanMakesZeroRequests(t *testing.T) {
	tr := &countingTransport{deny: true}
	swapTransport(t, tr)
	if code, _, stderr := runScanArgs(t, fixturePath("clean-app")); code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if tr.calls != 0 {
		t.Fatalf("plain scan made %d requests, want 0", tr.calls)
	}
}

// With --refresh the scanner makes exactly the catalog request and
// nothing else.
func TestScanRefreshMakesOnlyTheCatalogRequest(t *testing.T) {
	tr := &countingTransport{}
	swapTransport(t, tr)
	code, _, stderr := runScanArgs(t, "-refresh", fixturePath("clean-app"))
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if tr.calls != 1 {
		t.Fatalf("scan with --refresh made %d requests, want exactly the catalog request", tr.calls)
	}
}

// A failed refresh degrades to local prices instead of failing the scan.
func TestScanRefreshFailureFallsBack(t *testing.T) {
	tr := &countingTransport{deny: true}
	swapTransport(t, tr)
	code, stdout, stderr := runScanArgs(t, "-refresh", fixturePath("clean-app"))
	if code != ExitClean {
		t.Fatalf("exit = %d, want clean despite the failed refresh", code)
	}
	if !strings.Contains(stderr, "catalog refresh failed") {
		t.Errorf("stderr = %q, want a refresh failure note", stderr)
	}
	if !strings.Contains(stdout, "Keep the models you have.") {
		t.Errorf("stdout = %q, want the scan to have completed", stdout)
	}
}

func TestCatalogRefreshCommand(t *testing.T) {
	tr := &countingTransport{}
	swapTransport(t, tr)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"catalog", "refresh"}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "cached catalog") {
		t.Errorf("stdout = %q", stdout.String())
	}
	if tr.calls != 1 {
		t.Errorf("refresh made %d requests, want 1", tr.calls)
	}
}

func TestCatalogRefreshOfflineIsAContradiction(t *testing.T) {
	tr := &countingTransport{deny: true}
	swapTransport(t, tr)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"catalog", "refresh", "-offline"}, &stdout, &stderr); code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	if tr.calls != 0 {
		t.Errorf("offline refresh made %d requests, want 0", tr.calls)
	}
}
