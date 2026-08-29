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
		Uses        string            `yaml:"uses"`
		With        map[string]string `yaml:"with"`
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
			for _, want := range []string{`"$IMAGE:$TAG"`, `"$IMAGE:latest"`} {
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

// price-release exists because a tag pushed with GITHUB_TOKEN starts no
// workflow run: it has to call release rather than rely on the push.
func TestPriceReleaseCallsRelease(t *testing.T) {
	src := repoFile(t, ".github", "workflows", "price-release.yml")

	for _, want := range []string{
		"pull_request",
		"merged == true",
		"price-watch/",
		"-next-tag",
		"uses: ./.github/workflows/release.yml",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("price-release.yml is missing %q", want)
		}
	}

	rel := repoFile(t, ".github", "workflows", "release.yml")
	if !strings.Contains(rel, "workflow_call:") {
		t.Error("release.yml has no workflow_call trigger, so price-release cannot call it")
	}
	// The tag drives the stamped version, the notes range, and the
	// release itself; a leftover GITHUB_REF_NAME would be empty when
	// release runs as a called workflow.
	if strings.Contains(rel, "GITHUB_REF_NAME") {
		t.Error("release.yml still reads GITHUB_REF_NAME, which is wrong when called with an input tag")
	}
	if !strings.Contains(rel, "TAG: ${{ inputs.tag || github.ref_name }}") {
		t.Error("release.yml does not fall back to github.ref_name for a plain tag push")
	}
}

// The binaries are only half the release. The image needs the same
// called-workflow treatment for the same reason, or an automated price
// release ships new binaries against a stale image.
func TestPriceReleaseCallsImage(t *testing.T) {
	w := readWorkflow(t, "price-release.yml")
	job, ok := w.Jobs["image"]
	if !ok {
		t.Fatal("price-release.yml has no job that builds the image for the tag it pushed")
	}
	if job.Uses != "./.github/workflows/image.yml" {
		t.Errorf("price-release image job calls %q, want ./.github/workflows/image.yml", job.Uses)
	}
	if got := job.With["tag"]; got != "${{ needs.tag.outputs.tag }}" {
		t.Errorf("price-release image job passes tag %q, want the tag job's output", got)
	}
	// A called workflow's token can only narrow its caller's, and the
	// publish step pushes to GHCR.
	if job.Permissions["packages"] != "write" {
		t.Errorf("price-release image job packages permission is %q, want write", job.Permissions["packages"])
	}

	img := repoFile(t, ".github", "workflows", "image.yml")
	if !strings.Contains(img, "workflow_call:") {
		t.Error("image.yml has no workflow_call trigger, so price-release cannot call it")
	}
	// The same trap release.yml walked into: when called, the ref is the
	// merged pull request, so the tag has to come from the input.
	if strings.Contains(img, "GITHUB_REF_NAME") {
		t.Error("image.yml still reads GITHUB_REF_NAME, which is the caller's ref when called with an input tag")
	}
	if !strings.Contains(img, "ref: ${{ inputs.tag || github.ref }}") {
		t.Error("image.yml does not check out the input tag, so it would build the caller's ref")
	}

	// And the tag guard on the publish step has to admit a called run,
	// which carries no tag ref at all.
	for _, s := range readWorkflow(t, "image.yml").Jobs["image"].Steps {
		if strings.Contains(s.Run, "docker push") && !strings.Contains(s.If, "inputs.tag") {
			t.Errorf("step %q would skip the push when called with an input tag (if: %q)", s.Name, s.If)
		}
	}
}

// A price change is a patch release like any other. It used to push two
// tags, a four component one and a semver twin beside it, because four
// component tags are not semver and go install could not resolve them.
func TestPriceReleasePushesOneTag(t *testing.T) {
	src := repoFile(t, ".github", "workflows", "price-release.yml")
	for _, want := range []string{
		"-next-tag",
		`git tag "$next"`,
		`git push origin "$next"`,
		`echo "tag=$next"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("price-release.yml is missing %q", want)
		}
	}
	for _, gone := range []string{"-next-tags", "$twin", "$update"} {
		if strings.Contains(src, gone) {
			t.Errorf("price-release.yml still references %q from the two tag scheme", gone)
		}
	}
}

// The pin job is the whole answer to a tag never carrying its own
// checksums, and it is one deletion away from silently not running.
func TestReleasePinsAfterBuilding(t *testing.T) {
	src := repoFile(t, ".github", "workflows", "release.yml")
	for _, want := range []string{
		"pin:",
		"needs: release",
		"ref: main",
		"actions/download-artifact@v4",
		"name: SHA256SUMS",
		"tools/sync-manifests",
		"tools/major-tag",
		`git push -f origin "$major"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("release.yml pin job is missing %q", want)
		}
	}
}

// A price change reaches users only if its release pins too, which it
// does by calling release rather than duplicating it.
func TestPriceReleaseInheritsThePin(t *testing.T) {
	src := repoFile(t, ".github", "workflows", "price-release.yml")
	if !strings.Contains(src, "uses: ./.github/workflows/release.yml") {
		t.Error("price-release does not call release, so an auto shipped update would never repin")
	}
}

// The README's copy and paste example uses the floating tag, which is
// the sensible default the way actions/checkout@v4 is. An exact version
// is no longer wrong, only more work: it used to resolve to a tree
// carrying the previous release's digests, and now that the Action pins
// nothing but a version, a tag names its own release.
func TestReadmeUsesTheFloatingTag(t *testing.T) {
	src := repoFile(t, "README.md")
	if !strings.Contains(src, "MithrilBytes/overwater@v2\n") {
		t.Error("README does not hand readers the floating tag in its example")
	}
}

// The README lists the rules by name, and nothing kept that list honest.
// It described twelve while fifteen shipped, which is the kind of drift
// a reader has no way to detect: the page reads as authoritative and is
// simply behind. The rule files on disk are the source of truth, so the
// list is checked against them in both directions. Adding a rule without
// documenting it fails here, and so does documenting one that no longer
// ships.
func TestReadmeListsEveryShippedRule(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "rules"))
	if err != nil {
		t.Fatal(err)
	}
	shipped := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") || name == "estimates.yaml" {
			continue // estimates.yaml holds the cost assumptions, not a rule
		}
		shipped[strings.TrimSuffix(name, ".yaml")] = true
	}
	if len(shipped) == 0 {
		t.Fatal("found no rule files, so this test would pass vacuously")
	}

	readme := repoFile(t, "README.md")
	section := readmeSection(t, readme, "### Rules")
	listed := map[string]bool{}
	for _, m := range regexp.MustCompile("`([a-z0-9-]+)`").FindAllStringSubmatch(section, -1) {
		listed[m[1]] = true
	}

	for id := range shipped {
		if !listed[id] {
			t.Errorf("rules/%s.yaml ships and the README does not list it", id)
		}
	}
	for id := range listed {
		if !shipped[id] {
			t.Errorf("the README lists %q and no such rule ships", id)
		}
	}
}

// readmeSection returns the body under a heading, up to the next heading
// at the same level or above.
func readmeSection(t *testing.T, src, heading string) string {
	t.Helper()
	start := strings.Index(src, heading)
	if start < 0 {
		t.Fatalf("README has no %q section", heading)
	}
	body := src[start+len(heading):]
	if end := regexp.MustCompile(`(?m)^#{1,3} `).FindStringIndex(body); end != nil {
		body = body[:end[0]]
	}
	return body
}

// dogfood used the Action's download path, which fetches the last
// release rather than the tree under test, so a change in the commit
// being tested was not actually exercised. It also broke outright in
// the window between an action.yml bump and its tag, when the version
// the Action names has no release yet. It builds what it tests now.
func TestDogfoodTestsThisTree(t *testing.T) {
	src := repoFile(t, ".github", "workflows", "dogfood.yml")
	if !strings.Contains(src, "go build -o") {
		t.Error("dogfood does not build the binary it tests")
	}
	jobs := strings.Count(src, "uses: ./")
	if jobs == 0 {
		t.Fatal("dogfood no longer runs the Action at all")
	}
	if got := strings.Count(src, "binary: ${{ runner.temp }}/overwater"); got != jobs {
		t.Errorf("%d dogfood jobs run the Action and %d pass it a built binary; "+
			"the rest download the last release", jobs, got)
	}
}

// The install examples name a release, and nothing kept them current:
// both pages still said v2.6.0 after v2.7.0 shipped. The site rewrites
// its copy from the releases API at load, so the baked value is what a
// reader sees when that call fails or scripts are off, which makes it
// worth being right rather than approximately right.
//
// action.yml is the tree's own record of the release being cut, and the
// release refuses to build when it disagrees with the tag, so checking
// against it ties the docs to the same anchor.
func TestInstallExamplesNameTheCurrentRelease(t *testing.T) {
	m := regexp.MustCompile(`version="(v[0-9]+\.[0-9]+\.[0-9]+)"`).
		FindStringSubmatch(repoFile(t, "action.yml"))
	if m == nil {
		t.Fatal(`action.yml has no version="vX.Y.Z" to check the docs against`)
	}
	version := m[1]

	readme := repoFile(t, "README.md")
	if !strings.Contains(readme, "sh install.sh "+version) {
		t.Errorf("README's install example does not name %s", version)
	}
	if other := regexp.MustCompile(`sh install\.sh (v[0-9]+\.[0-9]+\.[0-9]+)`).
		FindAllStringSubmatch(readme, -1); len(other) != 1 || other[0][1] != version {
		t.Errorf("README install examples = %v, want exactly one naming %s", other, version)
	}

	site := repoFile(t, "site", "index.html")
	baked := regexp.MustCompile(`data-version>(v[0-9]+\.[0-9]+\.[0-9]+)<`).
		FindAllStringSubmatch(site, -1)
	if len(baked) == 0 {
		t.Fatal("the site has no baked [data-version] value to fall back on")
	}
	for _, b := range baked {
		if b[1] != version {
			t.Errorf("a site install example is baked at %s, want %s", b[1], version)
		}
	}
}
