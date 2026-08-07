package cli

import (
	"fmt"
	"io"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/MithrilBytes/overwater/rules"
)

// runExplain prints one rule, or lists the ids when given none.
func runExplain(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		fmt.Fprintln(stderr, "overwater: explain takes one rule id")
		return ExitError
	}
	engine, err := rules.Load()
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	loaded := slices.Clone(engine.Rules)
	sort.Slice(loaded, func(i, j int) bool { return loaded[i].ID < loaded[j].ID })
	if len(args) == 0 {
		fmt.Fprint(stdout, "Rules, for overwater explain <rule-id>:\n\n")
		printRuleIDs(stdout, loaded)
		return ExitClean
	}
	for _, r := range loaded {
		if r.ID == args[0] {
			fmt.Fprint(stdout, explainRule(r))
			return ExitClean
		}
	}
	fmt.Fprintf(stderr, "overwater: no rule %q. Rules are:\n\n", args[0])
	printRuleIDs(stderr, loaded)
	return ExitError
}

func printRuleIDs(w io.Writer, loaded []rules.Rule) {
	for _, r := range loaded {
		fmt.Fprintf(w, "  %s\n", r.ID)
	}
	fmt.Fprintln(w)
}

// explainRule renders one rule from its own YAML: the predicate under
// the keys it is written with, then the sentences the verdict prints.
func explainRule(r rules.Rule) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s, %s confidence)\n", r.ID, r.Kind, r.Confidence)
	if lines := whenLines(r.When); len(lines) > 0 {
		b.WriteString("\nLooks for:\n")
		for _, line := range lines {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Candidate: %s\n", r.Candidate.Note)
	fmt.Fprintf(&b, "Tripwire:  %s\n", r.Tripwire)
	if r.Flag != "" {
		fmt.Fprintf(&b, "Flag:      %s\n", r.Flag)
	}
	return b.String()
}

// whenLines renders the constraining fields of a predicate as
// "yaml_key: value". Reflection rather than a field by field switch,
// because those keys are also the ones .overwater.yaml thresholds name,
// and a switch would silently omit a predicate field added later.
func whenLines(when rules.When) []string {
	v := reflect.ValueOf(when)
	t := v.Type()
	var lines []string
	for i := 0; i < t.NumField(); i++ {
		f := v.Field(i)
		if f.IsZero() {
			continue // an absent field does not constrain
		}
		if f.Kind() == reflect.Pointer {
			f = f.Elem()
		}
		key, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		lines = append(lines, fmt.Sprintf("%s: %s", key, fieldValue(f)))
	}
	return lines
}

func fieldValue(f reflect.Value) string {
	if f.Kind() != reflect.Slice {
		return fmt.Sprintf("%v", f.Interface())
	}
	parts := make([]string, f.Len())
	for i := range parts {
		parts[i] = fmt.Sprintf("%v", f.Index(i).Interface())
	}
	return strings.Join(parts, ", ")
}
