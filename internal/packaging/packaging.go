// Package packaging renders the Homebrew, scoop, and winget manifests
// for a release from that release's SHA256SUMS. The manifests pin one
// version and the per platform sha256 the same way action.yml does, and
// are generated, never hand edited.
package packaging

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Release assets, named as the release workflow names them in
// SHA256SUMS.
const (
	assetDarwinAMD64  = "overwater_darwin_amd64"
	assetDarwinARM64  = "overwater_darwin_arm64"
	assetLinuxAMD64   = "overwater_linux_amd64"
	assetLinuxARM64   = "overwater_linux_arm64"
	assetWindowsAMD64 = "overwater_windows_amd64.exe"
)

// Assets lists every release asset a manifest refers to. Every one of
// them must appear in SHA256SUMS before anything is written.
var Assets = []string{
	assetDarwinAMD64,
	assetDarwinARM64,
	assetLinuxAMD64,
	assetLinuxARM64,
	assetWindowsAMD64,
}

const (
	repoURL = "https://github.com/MithrilBytes/overwater"
	// Kept under 80 characters and free of a trailing period so the
	// same sentence passes Homebrew's audit and winget's schema.
	description = "Flag LLM call sites that use more model than the task needs"
)

var (
	versionRe = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+)$`)
	sha256Re  = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

// ParseSums reads a release's SHA256SUMS. It accepts the sha256sum and
// shasum output format, with or without the binary mode asterisk, and
// ignores blank and comment lines.
func ParseSums(r io.Reader) (map[string]string, error) {
	sums := make(map[string]string)
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 2 {
			return nil, fmt.Errorf("SHA256SUMS line %d: want %q, got %q", n, "<sha256>  <file>", text)
		}
		hash, name := fields[0], path.Base(strings.TrimPrefix(fields[1], "*"))
		if !sha256Re.MatchString(hash) {
			return nil, fmt.Errorf("SHA256SUMS line %d: %q is not a 64 character hex sha256", n, hash)
		}
		if _, dup := sums[name]; dup {
			return nil, fmt.Errorf("SHA256SUMS lists %s twice; use the file the release published, unedited", name)
		}
		sums[name] = strings.ToLower(hash)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading SHA256SUMS: %w", err)
	}
	if len(sums) == 0 {
		return nil, fmt.Errorf("SHA256SUMS has no checksum lines")
	}
	return sums, nil
}

// view is what the manifest templates read.
type view struct {
	Version      string // 2.1.0, as the package managers want it
	Tag          string // v2.1.0, as the release URL wants it
	Repo         string
	Description  string
	DarwinAMD64  string
	DarwinARM64  string
	LinuxAMD64   string
	LinuxARM64   string
	WindowsAMD64 string
	// WindowsUpper is the same checksum uppercased, which is how the
	// winget community repository writes InstallerSha256.
	WindowsUpper string
}

// Render returns every generated manifest keyed by its path relative to
// the repository root. A version that is not vX.Y.Z, or a SHA256SUMS
// missing any release asset, is an error: a manifest with an empty
// checksum installs whatever the URL happens to serve.
func Render(version string, sums map[string]string) (map[string]string, error) {
	m := versionRe.FindStringSubmatch(strings.TrimSpace(version))
	if m == nil {
		return nil, fmt.Errorf("version %q is not vX.Y.Z; pass the release tag, for example v2.1.0", version)
	}
	var missing []string
	for _, a := range Assets {
		if sums[a] == "" {
			missing = append(missing, a)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("SHA256SUMS has no line for %s; use the SHA256SUMS published with %s", strings.Join(missing, ", "), version)
	}
	v := view{
		Version:      m[1],
		Tag:          "v" + m[1],
		Repo:         repoURL,
		Description:  description,
		DarwinAMD64:  sums[assetDarwinAMD64],
		DarwinARM64:  sums[assetDarwinARM64],
		LinuxAMD64:   sums[assetLinuxAMD64],
		LinuxARM64:   sums[assetLinuxARM64],
		WindowsAMD64: sums[assetWindowsAMD64],
		WindowsUpper: strings.ToUpper(sums[assetWindowsAMD64]),
	}
	out := make(map[string]string, len(manifests))
	for name, tmpl := range manifests {
		var b strings.Builder
		if err := tmpl.Execute(&b, v); err != nil {
			return nil, fmt.Errorf("rendering %s: %w", name, err)
		}
		out[name] = b.String()
	}
	return out, nil
}

// flakeVersionRe matches the one line of flake.nix that names a
// release. The flake builds from source, so it carries no checksums,
// and its vendorHash is a value only nix can compute: the version is
// rewritten in place to leave the rest of the file alone.
var flakeVersionRe = regexp.MustCompile(`(?m)^(\s*version\s*=\s*)"[^"]*"(\s*;.*)$`)

// BumpFlake returns flake.nix with its version set to the release's,
// leaving every other line, including vendorHash, alone.
func BumpFlake(src, version string) (string, error) {
	m := versionRe.FindStringSubmatch(strings.TrimSpace(version))
	if m == nil {
		return "", fmt.Errorf("version %q is not vX.Y.Z; pass the release tag, for example v2.1.0", version)
	}
	if !flakeVersionRe.MatchString(src) {
		return "", fmt.Errorf(`flake.nix has no 'version = "..."' line to update; restore one or the flake will pin a stale release`)
	}
	return flakeVersionRe.ReplaceAllString(src, `${1}"`+m[1]+`"${2}`), nil
}
