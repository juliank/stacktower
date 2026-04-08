package dotnet

import (
	"testing"
)

func TestNuGetMatcher_ParseVersion(t *testing.T) {
	m := NuGetMatcher{}

	tests := []struct {
		input   string
		wantNil bool
		wantStr string // expected .String() when non-nil
	}{
		{"1.0.0", false, "1.0.0"},
		{"9.0.9", false, "9.0.9"},
		{"10.0.5", false, "10.0.5"},
		{"4.3.1", false, "4.3.1"},
		{"1.0.0.0", false, "1.0.0"}, // 4-part: revision ignored
		{"1.0.0-beta.1", false, "1.0.0-beta.1"},
		{"  2.0.0  ", false, "2.0.0"}, // whitespace trimmed
		{"not-a-version", true, ""},
		{"", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := m.ParseVersion(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseVersion(%q) = %v, want nil", tt.input, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParseVersion(%q) = nil, want non-nil", tt.input)
			}
			if got.String() != tt.wantStr {
				t.Errorf("ParseVersion(%q).String() = %q, want %q", tt.input, got.String(), tt.wantStr)
			}
		})
	}
}

func TestNuGetMatcher_ParseVersion_NumericOrdering(t *testing.T) {
	// The key regression: 10.x.x must sort after 9.x.x (not before, as with SimpleVersion)
	m := NuGetMatcher{}

	// Constraint >= 10.0.5 should NOT be satisfied by 9.0.9
	cond := m.ParseConstraint("[10.0.5, )")
	if cond == nil {
		t.Fatal("ParseConstraint returned nil")
	}
	v9 := m.ParseVersion("9.0.9")
	v10 := m.ParseVersion("10.0.5")
	if v9 == nil || v10 == nil {
		t.Fatal("unexpected nil version")
	}
	if cond.Satisfies(v9) {
		t.Error("9.0.9 should NOT satisfy [10.0.5, ) — SimpleVersion string ordering bug would cause this")
	}
	if !cond.Satisfies(v10) {
		t.Error("10.0.5 should satisfy [10.0.5, )")
	}
}

func TestNuGetMatcher_ParseConstraint(t *testing.T) {
	m := NuGetMatcher{}

	tests := []struct {
		name       string
		constraint string
		wantNil    bool // true = expect nil (any version)
		// For non-nil: check that the condition is not empty (we can't easily introspect further)
	}{
		{"empty string", "", true},
		{"whitespace only", "   ", true},

		// NuGet bracket notation
		{"min inclusive open max", "[4.0.0, )", false},
		{"min exclusive open max", "(4.0.0, )", false},
		{"max inclusive", "(, 5.0.0]", false},
		{"max exclusive", "(, 5.0.0)", false},
		{"min+max inclusive+exclusive", "[1.0.0, 2.0.0)", false},
		{"min+max both inclusive", "[1.0.0, 2.0.0]", false},
		{"min+max both exclusive", "(1.0.0, 2.0.0)", false},
		{"exact version bracket", "[1.2.3]", false},

		// Bare version (treated as >= min)
		{"bare version", "1.2.3", false},
		{"bare version with spaces", "  2.0.0  ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.ParseConstraint(tt.constraint)
			if tt.wantNil && got != nil {
				t.Errorf("ParseConstraint(%q) = %v, want nil", tt.constraint, got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("ParseConstraint(%q) = nil, want non-nil condition", tt.constraint)
			}
		})
	}
}

func TestNugetRangeToSemver(t *testing.T) {
	tests := []struct {
		input  string
		want   string
		wantOK bool
	}{
		{"[4.0.0, )", ">= 4.0.0", true},
		{"(4.0.0, )", "> 4.0.0", true},
		{"(, 5.0.0]", "<= 5.0.0", true},
		{"(, 5.0.0)", "< 5.0.0", true},
		{"[1.0.0, 2.0.0)", ">= 1.0.0 < 2.0.0", true},
		{"[1.0.0, 2.0.0]", ">= 1.0.0 <= 2.0.0", true},
		{"(1.0.0, 2.0.0)", "> 1.0.0 < 2.0.0", true},
		{"[1.2.3]", "= 1.2.3", true},
		// Invalid / non-bracket inputs
		{"1.2.3", "", false},
		{"", "", false},
		{"[", "", false},
		{"(1.0.0)", "", false}, // exclusive open, no comma — not a valid range
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := nugetRangeToSemver(tt.input)
			if ok != tt.wantOK {
				t.Errorf("nugetRangeToSemver(%q) ok=%v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("nugetRangeToSemver(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
