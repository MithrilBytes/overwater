// Package scan implements the detection layers: manifests, model strings,
// and call site shape. It reads files and emits typed signals; deciding
// what counts as wasteful belongs to the rules engine.
//
// One pass over a repository runs these stages:
//
//	walk      reads every scannable file into the analyzer (walk.go)
//	mask      blanks comments and string interiors, giving each file an
//	          "all" view for bracket counting and a "prose" view where
//	          only short syntax level strings survive (mask.go)
//	find      matches catalog ids and aliases to locate model
//	          references (modelstring.go)
//	region    bounds each reference by its enclosing call extent, or by
//	          a line window when no extent is found (extent.go)
//	shape     reads call parameters out of the region, by regex
//	          (shape.go) and, where the language allows, by structural
//	          parse (call.go, props.go, builder.go)
//	classify  scores an archetype from the shape, the system prompt, and
//	          the enclosing function name (archetype.go)
//	fan in    indexes the repo's function definitions and calls, counts
//	          how many places reach each site through the function that
//	          holds it, and reads the models a wrapper's callers pass
//	          for a model parameter (fanin.go)
//
// The analyzer (analyzer.go) holds every walked file for the whole pass,
// so prompt and constant resolution can follow imports across the repo
// (prompt.go, imports.go) and config values can be traced to the code
// that reads them (trace.go).
package scan
