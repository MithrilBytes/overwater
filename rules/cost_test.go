package rules

import (
	"testing"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/scan"
)

// A response schema is a cap the model cannot talk past, so the output
// estimate scales with the fields it has to fill rather than sitting at
// default_output. Numbers are the shipped estimates.yaml: 10 envelope,
// 40 per field, 8 per enum field, 400 default output.
func TestSchemaBoundsOutputTokens(t *testing.T) {
	e, _ := loadEngine(t)
	def := e.Est.Tokens.DefaultOutput
	cases := []struct {
		name  string
		shape scan.Shape
		want  int
	}{
		{"no schema read", scan.Shape{}, def},
		{"one enum label", scan.Shape{SchemaEnumOnly: true, SchemaFields: 1}, 18},
		{"two enum labels", scan.Shape{SchemaEnumOnly: true, SchemaFields: 2}, 26},
		{"three field record", scan.Shape{SchemaMultiField: true, SchemaFields: 3}, 130},
		{"wide schema never raises", scan.Shape{SchemaMultiField: true, SchemaFields: 40}, def},
		{"one free text field is not a bound", scan.Shape{JSONSchema: true, SchemaFields: 1}, def},
		{"a tighter max_tokens still wins",
			scan.Shape{SchemaMultiField: true, SchemaFields: 3, MaxTokens: intPtr(50)}, 50},
		{"a loose max_tokens does not",
			scan.Shape{SchemaMultiField: true, SchemaFields: 3, MaxTokens: intPtr(1024)}, 130},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := e.outputTokens(scan.Site{Shape: tc.shape}); got != tc.want {
				t.Errorf("outputTokens = %d, want %d", got, tc.want)
			}
		})
	}
}

// The bound reaches the dollars on both sides of a finding: the current
// spend and the cached candidate price the same output.
func TestSchemaBoundPricesTheCall(t *testing.T) {
	e, _ := loadEngine(t)
	m := &catalog.Model{
		ID: "m", InputPerMtok: 1, OutputPerMtok: 100,
		CacheReadPerMtok: 0.1, CacheWritePerMtok: 1.25,
	}
	// One enum field: 18 output tokens, not 400.
	enum := scan.Site{Archetype: scan.ArchetypeClassification, Shape: scan.Shape{
		SchemaEnumOnly: true, SchemaFields: 1, SystemPromptChars: 4000,
	}}
	free := enum
	free.Shape.SchemaEnumOnly, free.Shape.SchemaFields = false, 0
	if bounded, unbounded := e.monthlyUSD(m, enum, 1000), e.monthlyUSD(m, free, 1000); bounded >= unbounded {
		t.Errorf("monthlyUSD bounded = %g, unbounded = %g, want the schema to cost less", bounded, unbounded)
	}
	if bounded, unbounded := e.cachedMonthlyUSD(m, enum, 1000), e.cachedMonthlyUSD(m, free, 1000); bounded >= unbounded {
		t.Errorf("cachedMonthlyUSD bounded = %g, unbounded = %g, want the schema to cost less", bounded, unbounded)
	}
}

// A schema_output knob left at zero would price a five field
// extraction at the envelope alone, so the estimates refuse to load.
func TestEstimatesRejectZeroSchemaOutput(t *testing.T) {
	e, _ := loadEngine(t)
	if err := e.Est.validate(); err != nil {
		t.Fatalf("shipped estimates do not validate: %v", err)
	}
	for _, field := range []string{"envelope", "per_field", "per_enum_field"} {
		est := e.Est
		switch field {
		case "envelope":
			est.Tokens.SchemaOutput.Envelope = 0
		case "per_field":
			est.Tokens.SchemaOutput.PerField = 0
		case "per_enum_field":
			est.Tokens.SchemaOutput.PerEnumField = 0
		}
		if err := est.validate(); err == nil {
			t.Errorf("%s at zero validated, want an error", field)
		}
	}
}
