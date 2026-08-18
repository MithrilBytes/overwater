package scan

import (
	"path"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/MithrilBytes/overwater/catalog"
)

// Fan in: how many places in the repo reach a call site through the
// function that holds it, which models those callers pass in, and what
// they ask the model to do.
//
// A repo that centralizes its LLM calls behind one helper has a single
// call site; the traffic is at the helper's callers, and so is the
// model when the helper takes it as a parameter. Layer 5 builds a repo
// wide index of definitions and calls, matches by name only, and
// refuses to guess when a name is defined more than once.
//
// There is no cross language resolution: names land in one language
// family only by accident of extension.

const (
	// paramsGap bounds the distance from a function name to its
	// parameter list, which covers "= async (" and Go receivers.
	paramsGap = 64
	// callerHops bounds how far a model value is chased back through
	// wrappers that pass their own parameter along.
	callerHops = 3
)

// Fan in statuses. Only exact and tests carry a counted number; the
// rest report 1 because the caller set could not be established, and 1
// there is a floor rather than a measurement. Only exact is traffic, so
// it is the only one estimates.yaml multiplies by.
const (
	FanInDirect     = "direct"     // the site is not inside any function
	FanInExact      = "exact"      // the enclosing function has counted production callers
	FanInAmbiguous  = "ambiguous"  // the function name is defined more than once
	FanInUnresolved = "unresolved" // no caller of the enclosing function is visible
	FanInTests      = "tests"      // every caller is a test or a fixture, so none is traffic
)

// CallerModel is one model that callers of the enclosing wrapper pass
// in, with the number of call sites passing it.
type CallerModel struct {
	Ref     string // the string as callers write it
	ModelID string // catalog id, empty when unknown
	Known   bool
	Count   int
}

// funcParam is one declared parameter. entries holds the keys of a
// destructured object parameter, which callers fill by name.
type funcParam struct {
	name             string
	defStart, defEnd int // byte range of the default value, empty when defEnd == 0
	entries          []funcParam
}

// funcDef is one function definition and the extent of its body.
type funcDef struct {
	file       string
	name       string
	start, end int // byte range covering the definition and its body
	nameAt     int // byte offset of the name itself
	bodyStart  int // just past the parameter list
	params     []funcParam
}

// callRef is one place that names a function. test marks a caller that
// runs in CI rather than in production (emit.go): still a caller, and
// still evidence of what the function is for, but not traffic.
type callRef struct {
	file string
	open int // byte offset of the opening parenthesis
	test bool
}

// defsInFile holds one file's definitions sorted by start, alongside a
// running maximum of their ends so containment lookups can stop early.
type defsInFile struct {
	defs   []*funcDef
	maxEnd []int
}

// repoIndex is the whole repository's definitions and calls, by name.
// constOwners maps a file's constant name to the single function whose
// model parameter defaults to it; a constant two functions share maps
// to nil, since the site cannot be attributed to either.
type repoIndex struct {
	byName      map[string][]*funcDef
	byFile      map[string]*defsInFile
	calls       map[string][]callRef
	constOwners map[string]map[string]*funcDef
}

// indexedLang lists the extensions whose function definitions the index
// can read. Config and data files have no functions.
func indexedLang(p string) bool {
	switch path.Ext(p) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".py", ".ipynb", ".rb", ".go",
		".java", ".kt", ".kts", ".rs", ".scala", ".swift", ".cs", ".php",
		".c", ".h", ".cpp", ".cc":
		return true
	}
	return false
}

func (a *analyzer) index() *repoIndex {
	a.indexOnce.Do(func() { a.idx = a.buildIndex() })
	return a.idx
}

// buildIndex runs two parallel passes over the repo: definitions first,
// then the calls that name one of them. Results merge in sorted path
// order, so the index does not depend on which worker finishes first.
func (a *analyzer) buildIndex() *repoIndex {
	idx := &repoIndex{
		byName:      map[string][]*funcDef{},
		byFile:      map[string]*defsInFile{},
		calls:       map[string][]callRef{},
		constOwners: map[string]map[string]*funcDef{},
	}
	perFile := make([][]*funcDef, len(a.paths))
	parallelOver(len(a.paths), func(i int) { perFile[i] = a.fileDefs(a.paths[i]) })
	for i, defs := range perFile {
		if len(defs) == 0 {
			continue
		}
		entry := &defsInFile{defs: defs, maxEnd: make([]int, len(defs))}
		running := 0
		owners := map[string]*funcDef{}
		for j, d := range defs {
			idx.byName[d.name] = append(idx.byName[d.name], d)
			running = max(running, d.end)
			entry.maxEnd[j] = running
			if name := modelDefaultConst(a.byPath[d.file], d); name != "" {
				if _, taken := owners[name]; taken {
					owners[name] = nil
				} else {
					owners[name] = d
				}
			}
		}
		idx.byFile[a.paths[i]] = entry
		if len(owners) > 0 {
			idx.constOwners[a.paths[i]] = owners
		}
	}

	type named struct {
		name string
		ref  callRef
	}
	perCalls := make([][]named, len(a.paths))
	parallelOver(len(a.paths), func(i int) {
		p := a.paths[i]
		if !indexedLang(p) {
			return
		}
		var out []named
		test := isTestPath(p)
		for _, c := range a.fileCalls(p, idx) {
			if _, ok := idx.byName[c.name]; ok {
				out = append(out, named{c.name, callRef{file: p, open: c.open, test: test}})
			}
		}
		perCalls[i] = out
	})
	for _, calls := range perCalls {
		for _, c := range calls {
			idx.calls[c.name] = append(idx.calls[c.name], c.ref)
		}
	}
	return idx
}

// parallelOver runs fn over 0..n-1, striding rather than handing each
// index over a channel: the per file work is smaller than a channel
// round trip.
func parallelOver(n int, fn func(i int)) {
	if n < 1 {
		return
	}
	workers := max(1, min(runtime.GOMAXPROCS(0), n))
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for i := start; i < n; i += workers {
				fn(i)
			}
		}(w)
	}
	wg.Wait()
}

// fileDefs reads one file's function definitions off the all view, so
// a definition written inside a prompt does not count.
func (a *analyzer) fileDefs(p string) []*funcDef {
	if !indexedLang(p) {
		return nil
	}
	content := a.byPath[p]
	all := a.masked(p).all
	seen := map[int]bool{}
	var defs []*funcDef
	add := func(start, nameAt, nameEnd int) {
		name := all[nameAt:nameEnd]
		if funcNameStopwords[name] || seen[nameAt] {
			return
		}
		seen[nameAt] = true
		d := &funcDef{file: p, name: name, start: start, nameAt: nameAt, bodyStart: nameEnd}
		if open, ok := paramsOpen(all, nameEnd); ok {
			if closer, ok := matchClose(all, open); ok {
				d.bodyStart = closer + 1
				d.params = parseParams(content, all, open+1, closer)
			}
		}
		defs = append(defs, d)
	}
	for _, re := range reKeywordFuncDefs {
		for _, m := range re.FindAllStringSubmatchIndex(all, -1) {
			add(m[0], m[2], m[3])
		}
	}
	for _, m := range cStyleDefs(all) {
		add(m[0], m[0], m[1])
	}
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].start != defs[j].start {
			return defs[i].start < defs[j].start
		}
		return defs[i].name < defs[j].name
	})
	closeDefs(all, a.lineStarts(p), defs)
	return defs
}

// The keyword anchored definition forms: every pattern in reFuncDefs
// but the C family one, which has no literal to anchor on. cStyleDefs
// scans for that one by byte, since an unanchored regex costs a full
// automaton pass over every file in the repo.
var reKeywordFuncDefs = reFuncDefs[:2]

// cStyleDefs finds C family definitions, name(args) {, matching the
// third pattern in reFuncDefs. Parameter lists longer than this bound
// are not definitions worth chasing.
const cDefParamsMax = 2000

func cStyleDefs(all string) [][2]int {
	var out [][2]int
	for brace := strings.IndexByte(all, '{'); brace >= 0; {
		if closer := trimWSBefore(all, brace); closer > 0 && all[closer-1] == ')' {
			if open, ok := openParenBefore(all, closer-1); ok {
				if start, name := identBefore(all, open); name != "" {
					out = append(out, [2]int{start, start + len(name)})
				}
			}
		}
		rel := strings.IndexByte(all[brace+1:], '{')
		if rel < 0 {
			break
		}
		brace += 1 + rel
	}
	return out
}

func trimWSBefore(all string, end int) int {
	for end > 0 && isWS(all[end-1]) {
		end--
	}
	return end
}

// openParenBefore walks back from a closing parenthesis to its opener,
// refusing any nested parenthesis in between.
func openParenBefore(all string, closer int) (int, bool) {
	limit := max(0, closer-cDefParamsMax)
	for i := closer - 1; i >= limit; i-- {
		switch all[i] {
		case '(':
			return i, true
		case ')':
			return 0, false
		}
	}
	return 0, false
}

// paramsOpen finds the parameter list opener that follows a function
// name. Ruby and Go definitions may have none, which is not an error.
func paramsOpen(all string, from int) (int, bool) {
	limit := min(len(all), from+paramsGap)
	for i := from; i < limit; i++ {
		switch all[i] {
		case '(':
			return i, true
		case '\n', ';', '{':
			return 0, false
		}
	}
	return 0, false
}

// closeDefs assigns each definition the end of its body by indentation:
// the body ends at the first later line indented no deeper than the
// definition. Braces are not load bearing, so one rule serves Python
// and the C family. A line holding only an opening brace is skipped for
// Allman style bodies, and no definition closes before its parameter
// list does, so a signature split over several lines stays open.
func closeDefs(all string, starts []int32, defs []*funcDef) {
	type open struct{ idx, indent int }
	var stack []open
	next := 0
	for ln := 0; ln < len(starts); ln++ {
		lineStart := int(starts[ln])
		lineEnd := len(all)
		if ln+1 < len(starts) {
			lineEnd = int(starts[ln+1])
		}
		text := all[lineStart:lineEnd]
		indent, meaningful := lineIndent(text)
		if meaningful {
			for len(stack) > 0 {
				top := stack[len(stack)-1]
				if indent > top.indent || lineStart < defs[top.idx].bodyStart {
					break
				}
				defs[top.idx].end = lineStart
				stack = stack[:len(stack)-1]
			}
		}
		for next < len(defs) && defs[next].start < lineEnd {
			if defs[next].start >= lineStart {
				stack = append(stack, open{next, indent})
			}
			next++
		}
	}
	for _, o := range stack {
		defs[o.idx].end = len(all)
	}
}

// lineIndent returns a line's indentation width and whether the line
// can close a body at all: blank lines and lone opening braces cannot.
func lineIndent(text string) (int, bool) {
	width := 0
	i := 0
	for ; i < len(text); i++ {
		switch text[i] {
		case ' ':
			width++
		case '\t':
			width += 4
		default:
			rest := strings.TrimSpace(text[i:])
			return width, rest != "" && rest != "{"
		}
	}
	return width, false
}

// byteRange is a half open span of a file.
type byteRange struct{ start, end int }

// topLevelParts splits a bracketed list at its depth zero commas.
func topLevelParts(all string, start, end int) []byteRange {
	if start < 0 || end > len(all) || start >= end {
		return nil
	}
	var parts []byteRange
	depth, from := 0, start
	for i := start; i < end; i++ {
		switch all[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, byteRange{from, i})
				from = i + 1
			}
		}
	}
	return append(parts, byteRange{from, end})
}

// parseParams reads a parameter list. A destructured object parameter
// keeps its keys as entries, since callers fill those by name.
func parseParams(content, all string, start, end int) []funcParam {
	var params []funcParam
	for _, part := range topLevelParts(all, start, end) {
		p := parseParam(content, all, part.start, part.end)
		if p.name != "" || len(p.entries) > 0 {
			params = append(params, p)
		}
	}
	return params
}

var reParamName = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*`)

func parseParam(content, all string, start, end int) funcParam {
	s, e := trimRange(all, start, end)
	if s >= e {
		return funcParam{}
	}
	if all[s] == '{' {
		if closer, ok := matchClose(all, s); ok && closer < e {
			return funcParam{entries: parseParams(content, all, s+1, closer)}
		}
		return funcParam{}
	}
	// Leading decorations: pointers, references, splats, annotations.
	for s < e && strings.IndexByte("*&.@", all[s]) >= 0 {
		s++
	}
	name := reParamName.FindString(all[s:e])
	if name == "" {
		return funcParam{}
	}
	p := funcParam{name: name}
	if eq := topLevelAssign(all, s+len(name), e); eq >= 0 {
		ds, de := trimRange(content, eq+1, e)
		if ds < de {
			p.defStart, p.defEnd = ds, de
		}
	}
	return p
}

// topLevelAssign returns the offset of a depth zero '=' that assigns,
// skipping the comparison and arrow spellings.
func topLevelAssign(all string, start, end int) int {
	depth := 0
	for i := start; i < end; i++ {
		switch all[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '=':
			if depth != 0 {
				continue
			}
			if i+1 < end && (all[i+1] == '=' || all[i+1] == '>') {
				continue
			}
			if i > start && strings.IndexByte("=!<>:", all[i-1]) >= 0 {
				continue
			}
			return i
		}
	}
	return -1
}

func trimRange(s string, start, end int) (int, int) {
	for start < end && isWS(s[start]) {
		start++
	}
	for end > start && isWS(s[end-1]) {
		end--
	}
	return start, end
}

// A call is a name followed by an opening parenthesis. Method calls
// count only through a self reference: client.messages.create is the
// SDK, not a repo function named create. Scanned by byte, not regex,
// for the same reason cStyleDefs is.
var selfReceivers = map[string]bool{"self": true, "this": true, "cls": true}

type fileCall struct {
	name string
	open int
}

func (a *analyzer) fileCalls(p string, idx *repoIndex) []fileCall {
	all := a.masked(p).all
	defined := map[int]bool{}
	if entry := idx.byFile[p]; entry != nil {
		for _, d := range entry.defs {
			defined[d.nameAt] = true
		}
	}
	var out []fileCall
	for open := strings.IndexByte(all, '('); open >= 0; {
		if start, name := identBefore(all, open); name != "" &&
			!funcNameStopwords[name] && !defined[start] &&
			(start == 0 || all[start-1] != '.' || selfReceivers[receiverOf(all, start-1)]) {
			out = append(out, fileCall{name: name, open: open})
		}
		rel := strings.IndexByte(all[open+1:], '(')
		if rel < 0 {
			break
		}
		open += 1 + rel
	}
	return out
}

func isIdentChar(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// identBefore reads the identifier that ends just before end, crossing
// whitespace, and returns where it starts. A name beginning with a
// digit is not an identifier.
func identBefore(all string, end int) (int, string) {
	for end > 0 && isWS(all[end-1]) {
		end--
	}
	start := end
	for start > 0 && isIdentChar(all[start-1]) {
		start--
	}
	if start == end || (all[start] >= '0' && all[start] <= '9') {
		return 0, ""
	}
	return start, all[start:end]
}

// receiverOf reads the identifier that ends at a dot.
func receiverOf(all string, dot int) string {
	_, name := identBefore(all, dot)
	return name
}

// enclosing returns the innermost definition holding an offset, or nil
// when the offset sits at file scope.
func (idx *repoIndex) enclosing(file string, off int) *funcDef {
	entry := idx.byFile[file]
	if entry == nil {
		return nil
	}
	i := sort.Search(len(entry.defs), func(i int) bool { return entry.defs[i].start > off }) - 1
	for ; i >= 0; i-- {
		if entry.maxEnd[i] <= off {
			return nil
		}
		if entry.defs[i].end > off {
			return entry.defs[i]
		}
	}
	return nil
}

func isModelName(name string) bool {
	return strings.Contains(normKey(name), "model")
}

// modelParamOf returns the parameter naming the model, its position in
// the parameter list, and its position inside a destructured object
// parameter, which is -1 for a plain positional parameter.
func modelParamOf(d *funcDef) (*funcParam, int, int) {
	for i := range d.params {
		if isModelName(d.params[i].name) {
			return &d.params[i], i, -1
		}
		for j := range d.params[i].entries {
			if isModelName(d.params[i].entries[j].name) {
				return &d.params[i].entries[j], i, j
			}
		}
	}
	return nil, 0, 0
}

// applyFanIn is layer 5: every site learns how many places reach it,
// where its model is a wrapper's default, what callers pass instead,
// and, where the site said nothing about its task, what the callers ask
// for (archetype.go).
func (a *analyzer) applyFanIn(report *Report, names map[string]*catalog.Model) {
	idx := a.index()
	// Cached: two sites can share a wrapper.
	resolved := map[*funcDef][]CallerModel{}
	for i := range report.Sites {
		s := &report.Sites[i]
		s.FanIn, s.FanInStatus = 1, FanInDirect
		hit := a.hitOffsetIn(s.File, s.Line, s.Col)
		d := idx.enclosing(s.File, hit)
		viaConst := false
		if d == nil {
			d = a.constDefaultOwner(idx, s.File, hit)
			viaConst = d != nil
		}
		if d == nil {
			continue
		}
		s.FanInFunc = d.name
		if len(idx.byName[d.name]) > 1 {
			s.FanInStatus = FanInAmbiguous
			continue
		}
		prod, tests := productionCalls(idx.calls[d.name])
		switch {
		case len(prod) > 0:
			s.FanInStatus, s.FanIn = FanInExact, len(prod)
			// Layer 4 read this file alone, and a helper that takes the
			// prompt as an argument says nothing there about the task.
			// The callers say it outright (archetype.go).
			a.archetypeFromCallers(idx, s, tierOf(names, s), prod)
		case tests > 0:
			// Real callers, none of them traffic. The count is reported
			// rather than dropped, so a helper only its suite calls does
			// not read as one nobody calls at all.
			s.FanInStatus, s.FanIn = FanInTests, tests
		default:
			s.FanInStatus = FanInUnresolved
		}
		mp, _, _ := modelParamOf(d)
		if mp == nil {
			continue
		}
		if viaConst || (mp.defEnd > 0 && hit >= mp.defStart && hit < mp.defEnd) {
			models, done := resolved[d]
			if !done {
				models = a.callerModels(idx, d, names)
				resolved[d] = models
			}
			s.CallerModels = models
		}
	}
}

// productionCalls splits a caller set into the calls that run in
// production and a count of the rest. A test names a model to assert on
// it and calls a helper to exercise it, neither of which is monthly
// traffic; a well tested repository has more test callers than
// production ones, so counting them prices a helper at the size of its
// suite.
func productionCalls(refs []callRef) ([]callRef, int) {
	prod, tests := make([]callRef, 0, len(refs)), 0
	for _, c := range refs {
		if c.test {
			tests++
			continue
		}
		prod = append(prod, c)
	}
	return prod, tests
}

// tierOf is the catalog tier of a site's model, empty when the model is
// unknown, matching what layer 4 was given the first time.
func tierOf(names map[string]*catalog.Model, s *Site) string {
	if m := names[s.ModelID]; s.Known && m != nil {
		return m.Tier
	}
	return ""
}

var reAssignedName = regexp.MustCompile(
	`^\s*(?:export\s+)?(?:pub\s+)?(?:const|let|var|final|static|val)?\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*(?::[^=\n]*)?=`)

// modelDefaultConst returns the constant name a function's model
// parameter defaults to, empty when the default is a literal or absent.
func modelDefaultConst(content string, d *funcDef) string {
	mp, _, _ := modelParamOf(d)
	if mp == nil || mp.defEnd == 0 {
		return ""
	}
	return plainIdent(content[mp.defStart:mp.defEnd])
}

// constDefaultOwner finds the function whose model parameter defaults
// to the constant a file scope site defines. Without this hop a default
// model constant reads as a leaf, though its traffic is at the
// wrapper's callers.
func (a *analyzer) constDefaultOwner(idx *repoIndex, p string, hit int) *funcDef {
	owners := idx.constOwners[p]
	if len(owners) == 0 {
		return nil
	}
	content := a.byPath[p]
	starts := a.lineStarts(p)
	ln := sort.Search(len(starts), func(i int) bool { return int(starts[i]) > hit }) - 1
	if ln < 0 {
		return nil
	}
	lineStart := int(starts[ln])
	lineEnd := len(content)
	if ln+1 < len(starts) {
		lineEnd = int(starts[ln+1])
	}
	m := reAssignedName.FindStringSubmatchIndex(a.masked(p).prose[lineStart:lineEnd])
	if m == nil || lineStart+m[1] > hit {
		return nil
	}
	return owners[content[lineStart+m[2]:lineStart+m[3]]]
}

// callerScan tallies the models a wrapper's callers pass in.
type callerScan struct {
	a     *analyzer
	idx   *repoIndex
	names map[string]*catalog.Model
	tally map[string]int
	seen  map[string]bool
}

// callerModels reports the models production callers pass for a
// wrapper's model parameter, most used first. Counts sum to at most the
// fan in: a caller whose argument could not be read is left out rather
// than guessed at, and so is one that only runs in CI.
func (a *analyzer) callerModels(idx *repoIndex, d *funcDef, names map[string]*catalog.Model) []CallerModel {
	cs := &callerScan{a: a, idx: idx, names: names, tally: map[string]int{}, seen: map[string]bool{}}
	cs.walk(d, 0)
	if len(cs.tally) == 0 {
		return nil
	}
	refs := make([]string, 0, len(cs.tally))
	for ref := range cs.tally {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if cs.tally[refs[i]] != cs.tally[refs[j]] {
			return cs.tally[refs[i]] > cs.tally[refs[j]]
		}
		return refs[i] < refs[j]
	})
	out := make([]CallerModel, 0, len(refs))
	for _, ref := range refs {
		cm := CallerModel{Ref: ref, Count: cs.tally[ref]}
		if m := names[ref]; m != nil {
			cm.ModelID, cm.Known = m.ID, true
		}
		out = append(out, cm)
	}
	return out
}

// walk credits every caller of d with the model it passes. A caller
// that passes its own parameter along is followed one hop further; the
// seen set makes recursion and mutual recursion terminate.
func (cs *callerScan) walk(d *funcDef, depth int) {
	key := d.file + "\x00" + d.name
	if depth > callerHops || cs.seen[key] {
		return
	}
	cs.seen[key] = true
	mp, pos, entry := modelParamOf(d)
	if mp == nil {
		return
	}
	fallback := ""
	if mp.defEnd > 0 {
		fallback = cs.a.resolveModelArg(cs.names, d.file, cs.a.byPath[d.file][mp.defStart:mp.defEnd])
	}
	prod, _ := productionCalls(cs.idx.calls[d.name])
	for _, c := range prod {
		raw, passed := cs.a.argAt(c, pos, entry)
		if !passed {
			if fallback != "" {
				cs.tally[fallback]++
			}
			continue
		}
		if ref := cs.a.resolveModelArg(cs.names, c.file, raw); ref != "" {
			cs.tally[ref]++
			continue
		}
		// The caller forwards its own parameter: its callers decide.
		if ident := plainIdent(raw); ident != "" {
			if outer := cs.idx.enclosing(c.file, c.open); outer != nil && hasParam(outer, ident) {
				cs.walk(outer, depth+1)
			}
		}
	}
}

func hasParam(d *funcDef, name string) bool {
	for i := range d.params {
		if d.params[i].name == name {
			return true
		}
		for j := range d.params[i].entries {
			if d.params[i].entries[j].name == name {
				return true
			}
		}
	}
	return false
}

// argAt returns the argument a call passes for a parameter, and whether
// it passed one at all. A keyword spelling wins over the position, so
// Python's model= is read where it is written.
func (a *analyzer) argAt(c callRef, pos, entry int) (string, bool) {
	content := a.byPath[c.file]
	all := a.masked(c.file).all
	if c.open >= len(all) || all[c.open] != '(' {
		return "", false
	}
	closer, ok := matchClose(all, c.open)
	if !ok {
		return "", false
	}
	parts := topLevelParts(all, c.open+1, closer)
	for _, part := range parts {
		eq := topLevelAssign(all, part.start, part.end)
		if eq < 0 {
			continue
		}
		ks, ke := trimRange(all, part.start, eq)
		if ks < ke && isModelName(all[ks:ke]) {
			vs, ve := trimRange(content, eq+1, part.end)
			return content[vs:ve], true
		}
	}
	if pos >= len(parts) {
		return "", false
	}
	s, e := trimRange(content, parts[pos].start, parts[pos].end)
	if s >= e {
		return "", false
	}
	if entry < 0 {
		return content[s:e], true
	}
	props := wrapperProps(content[s:e])
	if props == nil {
		return "", false
	}
	// Sorted, so an object argument carrying two model shaped keys
	// resolves the same way on every run.
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if isModelName(k) {
			return props[k], true
		}
	}
	return "", false
}

var reQuoted = regexp.MustCompile("(?s)[\"'`]([^\"'`\n]*)[\"'`]")

// resolveModelArg turns an argument as written into a model string: a
// literal, a literal inside a wrapping constructor, or a named
// constant. A value that neither the catalog knows nor looks like a
// model id reports empty, so wrapper plumbing cannot pose as a model.
func (a *analyzer) resolveModelArg(names map[string]*catalog.Model, file, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if m := reQuoted.FindStringSubmatch(raw); m != nil {
		if modelish(names, m[1]) {
			return m[1]
		}
		return ""
	}
	if ident := plainIdent(raw); ident != "" {
		if text, ok := a.resolveConstText(file, ident); ok && modelish(names, text) {
			return text
		}
	}
	return ""
}

func modelish(names map[string]*catalog.Model, s string) bool {
	return names[s] != nil || unknownModelRE.MatchString(s)
}

var rePlainIdent = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func plainIdent(raw string) string {
	raw = strings.TrimSpace(raw)
	if rePlainIdent.MatchString(raw) {
		return raw
	}
	return ""
}
