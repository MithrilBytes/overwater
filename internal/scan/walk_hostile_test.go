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
