package render

import (
	"bytes"
	"fmt"
	"html"
	"strings"

	"github.com/MithrilBytes/overwater/rules"
)

// htmlHead is the fixed page shell: inline CSS, a system font stack, and
// a dark variant behind prefers-color-scheme. No scripts and no external
// assets; the page must stay self contained.
const htmlHead = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Overwater verdict</title>
<style>
:root { color-scheme: light dark; }
body {
  font-family: system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  max-width: 46rem;
  margin: 2rem auto;
  padding: 0 1rem;
  line-height: 1.5;
  background: #ffffff;
  color: #1c1c1c;
}
h1 { font-size: 1.4rem; }
.assumption { color: #565656; }
.finding {
  border: 1px solid #d4d4d4;
  border-radius: 6px;
  padding: 0.75rem 1rem;
  margin: 1rem 0;
}
dl {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 0.3rem 1rem;
  margin: 0;
}
dt { font-weight: 600; }
dd { margin: 0; overflow-wrap: anywhere; }
.keep { font-size: 1.6rem; margin-top: 3rem; }
@media (prefers-color-scheme: dark) {
  body { background: #17181a; color: #d8d8d8; }
  .assumption { color: #9d9d9d; }
  .finding { border-color: #3b3b3b; }
}
</style>
</head>
<body>
<h1>Overwater verdict</h1>
`

const htmlFoot = "</body>\n</html>\n"

// HTML renders the verdict as one page: the same header facts as the
// markdown surface, then one card per finding.
func HTML(findings []rules.Finding, meta Meta) []byte {
	var b bytes.Buffer
	b.WriteString(htmlHead)
	if len(findings) == 0 {
		fmt.Fprintf(&b, "<p class=\"assumption\">Prices from catalog %s.</p>\n",
			html.EscapeString(meta.CatalogVersion))
		fmt.Fprintf(&b, "<p class=\"keep\">%s</p>\n", html.EscapeString(KeepVerdict))
		b.WriteString(htmlFoot)
		return b.Bytes()
	}
	fmt.Fprintf(&b,
		"<p class=\"assumption\">Prices from catalog %s. Costs are estimates at %s calls per month per call site; override with --volume.</p>\n",
		html.EscapeString(meta.CatalogVersion), comma(meta.CallsPerMonth))
	for _, f := range findings {
		b.WriteString(card(f))
	}
	b.WriteString(htmlFoot)
	return b.Bytes()
}

// card renders one finding as a definition list, same field order as
// block.
func card(f rules.Finding) string {
	var b strings.Builder
	head := f.Archetype
	if f.Evidence != "" {
		head += ": " + f.Evidence
	}
	b.WriteString("<section class=\"finding\">\n<dl>\n")
	row(&b, "Call site", fmt.Sprintf("%s:%d (%s; %s confidence)", f.File, f.Line, head, f.Confidence))
	row(&b, "Current", fmt.Sprintf("%s at ~$%d/mo at estimated volume", f.Model, f.MonthlyUSD))
	row(&b, "Candidate", f.CandidateText)
	row(&b, "Tripwire", f.Tripwire)
	if len(f.Flags) == 0 {
		row(&b, "Flag", "None")
	} else {
		for _, flag := range f.Flags {
			row(&b, "Flag", flag)
		}
	}
	b.WriteString("</dl>\n</section>\n")
	return b.String()
}

func row(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "<dt>%s</dt><dd>%s</dd>\n", label, html.EscapeString(value))
}
