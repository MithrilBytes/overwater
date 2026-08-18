package cli

import (
	"strings"
	"testing"
)

// A model the catalog does not carry is priced at nothing and produces
// no findings. Without a word on stderr that is indistinguishable from a
// repository that is genuinely fine, which is the worst answer a scanner
// can give: humanasllm pins deepseek-v4-flash and was told to keep the
// models it has.
func TestUnrecognizedModelIsReported(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, "ask.py", `from openai import OpenAI

client = OpenAI(base_url="https://api.deepseek.com")

def ask(q):
    return client.chat.completions.create(
        model="deepseek-v4-flash",
        messages=[{"role": "user", "content": q}],
    )
`)

	code, stdout, stderr := runScanArgs(t, dir)
	if code != ExitClean {
		t.Fatalf("exit = %d, want clean; an unpriced model is a note, not a failure", code)
	}
	if !strings.Contains(stderr, "not in catalog") ||
		!strings.Contains(stderr, "deepseek-v4-flash") {
		t.Errorf("stderr = %q, want the unrecognized model named", stderr)
	}
	if !strings.Contains(stdout, "Keep the models you have.") {
		t.Errorf("stdout = %q, want the null verdict untouched", stdout)
	}
}

// A config value that resolved to nothing is not a model id, it is
// whatever the file holds. Config tracing accepts any key mentioning
// MODEL or DEPLOYMENT whose value carries a digit or a dash, which
// describes MODEL_API_KEY and DEPLOYMENT_TOKEN exactly, and this note
// reaches the job log, the step summary and a pull request comment,
// where GitHub masks only values registered as Actions secrets. So the
// note names the key and leaves the value where it found it.
func TestUnrecognizedConfigValueIsNamedByKey(t *testing.T) {
	const canary = "b1f0a9e7-DEADBEEF-cafe-4242-fake-token"
	dir := t.TempDir()
	writeRepoFile(t, dir, ".env", "MODEL_API_KEY="+canary+"\n")
	writeRepoFile(t, dir, "main.go", "package main\n\n"+
		"func token() string { return os.Getenv(\"MODEL_API_KEY\") }\n")

	code, stdout, stderr := runScanArgs(t, dir)
	if code != ExitClean {
		t.Fatalf("exit = %d, want clean; stderr = %q", code, stderr)
	}
	if strings.Contains(stderr, canary) || strings.Contains(stdout, canary) {
		t.Errorf("the .env value was republished: stderr = %q, stdout = %q", stderr, stdout)
	}
	if !strings.Contains(stderr, ".env MODEL_API_KEY") {
		t.Errorf("stderr = %q, want the key that holds the value named", stderr)
	}
}

// Silence when every model is known, or the note becomes wallpaper.
func TestKnownModelsProduceNoNote(t *testing.T) {
	for _, fixture := range []string{"clean-app", "ts-chat-firehose", "py-extraction"} {
		t.Run(fixture, func(t *testing.T) {
			_, _, stderr := runScanArgs(t, fixturePath(fixture))
			if strings.Contains(stderr, "not in catalog") {
				t.Errorf("stderr = %q, want no unrecognized note", stderr)
			}
		})
	}
}

// A repository can name many models it never calls. The note says how
// many rather than printing all of them.
func TestUnrecognizedNoteIsBounded(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < maxUnrecognizedNamed+5; i++ {
		writeRepoFile(t, dir, string(rune('a'+i))+".py",
			"MODEL = \"deepseek-v9-"+strings.Repeat("x", i+1)+"\"\n")
	}
	_, _, stderr := runScanArgs(t, dir)
	if !strings.Contains(stderr, "and 5 more") {
		t.Errorf("stderr = %q, want the count of the ones it did not name", stderr)
	}
}
