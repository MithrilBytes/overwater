package release

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The release workflow, the image workflow, and the Dockerfile all ship
// the same thing, and nothing in CI compiles them together. Their
// agreement is pinned here: version stamp, Go toolchain, base image,
// default command, and the permissions each workflow holds.

func repoFile(t *testing.T, parts ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

type workflow struct {
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		Permissions map[string]string `yaml:"permissions"`
		Steps       []struct {
			Name string            `yaml:"name"`
			Uses string            `yaml:"uses"`
			Run  string            `yaml:"run"`
			If   string            `yaml:"if"`
			With map[string]string `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func readWorkflow(t *testing.T, name string) workflow {
	t.Helper()
	var w workflow
	if err := yaml.Unmarshal([]byte(repoFile(t, ".github", "workflows", name)), &w); err != nil {
		t.Fatalf("%s does not parse: %v", name, err)
	}
	if len(w.Jobs) == 0 {
		t.Fatalf("%s has no jobs", name)
	}
	return w
}

// stampRE finds the ldflags symbol the version is written into.
var stampRE = regexp.MustCompile(`-X ([\w./-]+)\.(\w+)=`)

// The Dockerfile and the release workflow have to write the version into
// the same symbol, and that symbol has to be a variable that still exists.
func TestVersionStampSymbolExists(t *testing.T) {
	docker := stampRE.FindStringSubmatch(repoFile(t, "Dockerfile"))
	if docker == nil {
		t.Fatal("Dockerfile does not stamp a version into any symbol")
	}
	release := stampRE.FindStringSubmatch(repoFile(t, ".github", "workflows", "release.yml"))
	if release == nil {
		t.Fatal("release.yml does not stamp a version into any symbol")
	}
	if docker[0] != release[0] {
		t.Errorf("Dockerfile stamps %q, release.yml stamps %q", docker[0], release[0])
	}

	module := regexp.MustCompile(`(?m)^module (\S+)`).FindStringSubmatch(repoFile(t, "go.mod"))
	if module == nil {
		t.Fatal("go.mod has no module line")
	}
	pkg, name := docker[1], docker[2]
	rel, ok := strings.CutPrefix(pkg, module[1]+"/")
	if !ok {
		t.Fatalf("stamped package %q is outside module %q", pkg, module[1])
	}
	dir := filepath.Join("..", "..", filepath.FromSlash(rel))
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("stamped package %q has no source at %s", pkg, dir)
	}
	decl := regexp.MustCompile(`(?m)^var ` + name + `\b`)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if decl.Match(raw) {
			return
		}
	}
	t.Errorf("nothing in %s declares var %s, so -ldflags -X %s.%s stamps nothing", dir, name, pkg, name)
}

// A builder newer or older than the go directive builds a different
// binary than CI and the release do.
func TestDockerfileBuilderMatchesGoMod(t *testing.T) {
	want := regexp.MustCompile(`(?m)^go (\S+)`).FindStringSubmatch(repoFile(t, "go.mod"))
	if want == nil {
		t.Fatal("go.mod has no go directive")
	}
	got := regexp.MustCompile(`FROM golang:(\S+?)(?:-\S+)? AS`).FindStringSubmatch(repoFile(t, "Dockerfile"))
	if got == nil {
		t.Fatal("Dockerfile has no golang builder stage")
	}
	if got[1] != want[1] {
		t.Errorf("Dockerfile builds on golang:%s, go.mod asks for go %s", got[1], want[1])
	}
}

// The runtime stage has to be static (the binary is CGO_ENABLED=0) and
// has to carry root CA certificates: catalog refresh is an HTTPS fetch,
// and a certificate-less base fails it as unverifiable.
func TestImageIsStaticWithCACerts(t *testing.T) {
	dockerfile := repoFile(t, "Dockerfile")
	if !strings.Contains(dockerfile, "CGO_ENABLED=0") {
		t.Error("Dockerfile does not build with CGO_ENABLED=0, so the binary is not static")
	}
	froms := regexp.MustCompile(`(?m)^FROM (\S+)`).FindAllStringSubmatch(dockerfile, -1)
	runtime := froms[len(froms)-1][1]
	switch {
	case strings.HasPrefix(runtime, "gcr.io/distroless/static"):
	case strings.Contains(dockerfile, "ca-certificates"):
	default:
		t.Errorf("runtime base %q carries no CA certificates and none are copied in", runtime)
	}
}

// The documented usage is a repo mounted at /repo, so the bare image has
// to scan it with no arguments.
func TestImageScansMountedVolume(t *testing.T) {
	dockerfile := repoFile(t, "Dockerfile")
	for _, want := range []string{
		`WORKDIR /repo`,
		`CMD ["scan", "/repo"]`,
	} {
		if !strings.Contains(dockerfile, want) {
			t.Errorf("Dockerfile is missing: %s", want)
		}
	}
	entry := regexp.MustCompile(`(?m)^ENTRYPOINT \["(\S+)"\]`).FindStringSubmatch(dockerfile)
	if entry == nil {
		t.Fatal("Dockerfile has no single-binary ENTRYPOINT")
	}
	if filepath.Base(entry[1]) != "overwater" {
		t.Errorf("entrypoint is %q, want the overwater binary", entry[1])
	}
	copied := regexp.MustCompile(`(?m)^COPY --from=\S+ \S+ (\S+)$`).FindAllStringSubmatch(dockerfile, -1)
	placed := false
	for _, c := range copied {
		if c[1] == entry[1] {
			placed = true
		}
	}
	if !placed {
		t.Errorf("no COPY --from lands a binary at the entrypoint path %s", entry[1])
	}
}

// Attestation needs id-token and attestations write. Anything beyond
// those and contents: write is more than the release job has any use for.
func TestReleaseAttestScope(t *testing.T) {
	w := readWorkflow(t, "release.yml")
	want := map[string]string{"contents": "write", "id-token": "write", "attestations": "write"}
	for k, v := range want {
		if w.Permissions[k] != v {
			t.Errorf("release.yml permissions[%s] = %q, want %q", k, w.Permissions[k], v)
		}
	}
	for k := range w.Permissions {
		if _, ok := want[k]; !ok {
			t.Errorf("release.yml grants %s, which the release does not need", k)
		}
	}

	var attested string
	for _, s := range w.Jobs["release"].Steps {
		if strings.HasPrefix(s.Uses, "actions/attest-build-provenance@") {
			attested = s.With["subject-path"]
		}
	}
	if attested == "" {
		t.Fatal("release.yml never runs actions/attest-build-provenance over the built binaries")
	}
	// Whatever it attests has to cover the binaries the release uploads,
	// not just the checksums file beside them.
	if !strings.Contains(attested, "dist/") || !strings.Contains(attested, "overwater") {
		t.Errorf("attested subject-path %q does not point at dist/overwater_*", attested)
	}
}

// The notes are generated now, so a hardcoded --notes would silently
// bypass this package.
func TestReleaseUsesGeneratedNotes(t *testing.T) {
	w := readWorkflow(t, "release.yml")
	var run string
	for _, s := range w.Jobs["release"].Steps {
		run += s.Run + "\n"
	}
	if !strings.Contains(run, "--notes-file") || regexp.MustCompile(`--notes\s`).MatchString(run) {
		t.Error("release.yml does not create the release from a --notes-file")
	}
	if !strings.Contains(run, "go run ./internal/release/cmd") {
		t.Error("release.yml does not render the notes with ./internal/release/cmd")
	}
	if !strings.Contains(run, "git log") {
		t.Error("release.yml does not feed the notes from git log")
	}
}

// The image job publishes with the workflow's own token, only on a tag,
// and tags both the version and latest.
func TestImagePublishesOnTagsOnly(t *testing.T) {
	raw := repoFile(t, ".github", "workflows", "image.yml")
	w := readWorkflow(t, "image.yml")
	job := w.Jobs["image"]
	if job.Permissions["packages"] != "write" {
		t.Errorf("image job packages permission is %q, want write", job.Permissions["packages"])
	}
	if job.Permissions["contents"] != "read" {
		t.Errorf("image job contents permission is %q, want read", job.Permissions["contents"])
	}

	var push, smoke bool
	for _, s := range job.Steps {
		switch {
		case strings.Contains(s.Run, "docker push"):
			push = true
			if !strings.Contains(s.If, "refs/tags/v") {
				t.Errorf("step %q pushes without a tag guard (if: %q)", s.Name, s.If)
			}
			for _, want := range []string{`"$IMAGE:$GITHUB_REF_NAME"`, `"$IMAGE:latest"`} {
				if strings.Count(s.Run, "docker push "+want) != 1 {
					t.Errorf("step %q does not push %s exactly once", s.Name, want)
				}
			}
		case strings.Contains(s.Run, "docker run"):
			smoke = true
			if !strings.Contains(s.Run, "/repo") {
				t.Errorf("step %q never runs the image against a repo volume", s.Name)
			}
		}
	}
	if !push {
		t.Error("image.yml never pushes the image")
	}
	if !smoke {
		t.Error("image.yml builds the image without ever running it")
	}
	if !strings.Contains(raw, "IMAGE: ghcr.io/mithrilbytes/overwater") {
		t.Error("image.yml does not publish to ghcr.io/mithrilbytes/overwater")
	}
	// A repository secret would not be the workflow's own token.
	if !strings.Contains(raw, "secrets.GITHUB_TOKEN") {
		t.Error("image.yml does not log in to ghcr with the workflow's GITHUB_TOKEN")
	}
}
