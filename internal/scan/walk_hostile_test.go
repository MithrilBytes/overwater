//go:build unix

package scan

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A hostile tree: a FIFO that would block ReadFile forever, a symlink to
// a file past the size cap, and a symlink escaping the root. Healthy
// files scan, hostiles are skipped, and the walk finishes fast.
func TestWalkSkipsHostileEntries(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "ok.py"), []byte("model = \"gpt-4o\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe.py"), 0o644); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	big := filepath.Join(outside, "big.py")
	if err := os.WriteFile(big, []byte(strings.Repeat("x = 1\n", 120000)), 0o644); err != nil {
		t.Fatal(err) // ~700KB, past the 512KB cap
	}
	if err := os.Symlink(big, filepath.Join(root, "big-link.py")); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.py")
	if err := os.WriteFile(secret, []byte("model = \"gpt-4o-mini\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "escape.py")); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	files, err := walk(root)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("walk took %v, want fast completion", elapsed)
	}
	if len(files) != 1 || files[0].path != "ok.py" {
		var got []string
		for _, f := range files {
			got = append(got, f.path)
		}
		t.Errorf("walked %v, want only ok.py", got)
	}
}

// An unreadable subdirectory is skipped, not fatal.
func TestWalkSkipsUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.py"), []byte("model = \"gpt-4o\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "hidden.py"), []byte("model = \"gpt-4o-mini\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	files, err := walk(root)
	if err != nil {
		t.Fatalf("walk: %v, want the unreadable dir skipped", err)
	}
	if len(files) != 1 || files[0].path != "ok.py" {
		var got []string
		for _, f := range files {
			got = append(got, f.path)
		}
		t.Errorf("walked %v, want only ok.py", got)
	}
}

// A missing root is still an error; only entries below a healthy root
// are skippable.
func TestWalkMissingRootErrors(t *testing.T) {
	if _, err := walk(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("walk of a missing root returned nil error")
	}
}

// A symlinked root is the caller's own argument, not a link inside the
// tree, and it is resolved before the walk. It used to be dropped like
// any other symlink, so a CI job pointed at a linked workspace scanned
// nothing and reported a clean repository, and scan link and scan link/
// gave opposite verdicts on the same bytes.
func TestWalkResolvesSymlinkedRoot(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.MkdirAll(filepath.Join(target, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "src", "app.py"), []byte("model = \"gpt-4o\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "rootlink")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	files, err := walk(link)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	// Reported in the caller's spelling: paths stay relative to the root
	// as given, never to the path it resolved to.
	if len(files) != 1 || files[0].path != "src/app.py" {
		var got []string
		for _, f := range files {
			got = append(got, f.path)
		}
		t.Errorf("walked %v, want src/app.py", got)
	}

	r, err := Analyze(link, mustCatalog(t))
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(r.Sites) != 1 || r.Sites[0].File != "src/app.py" {
		t.Errorf("sites = %+v, want the one call in src/app.py", r.Sites)
	}
	if r.Root != link {
		t.Errorf("report root = %q, want the root as the caller spelled it (%q)", r.Root, link)
	}
}

// A walk that admits nothing is not an error: an incremental run whose
// candidates were all deleted scans zero files and is right to. What it
// must not do is arrive at the verdict indistinguishable from a scan
// that read the repository and found it clean, so the count is carried
// out and the caller announces it.
func TestWalkOverZeroFilesIsCountedNotFailed(t *testing.T) {
	files, err := walk(t.TempDir())
	if err != nil {
		t.Fatalf("walk over an empty tree: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("walked %d files in an empty tree", len(files))
	}
	r, err := Analyze(t.TempDir(), mustCatalog(t))
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if r.Scanned != 0 {
		t.Errorf("report scanned = %d, want 0", r.Scanned)
	}
}
