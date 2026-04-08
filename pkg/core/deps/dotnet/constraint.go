package dotnet

import (
	"strings"

	"github.com/contriboss/pubgrub-go"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

// NuGetMatcher implements constraint matching for NuGet version ranges.
//
// NuGet version range syntax:
//   - Exact version:  [1.0.0]
//   - Min inclusive:  1.0.0  or  [1.0.0, )
//   - Range:          [1.0.0, 2.0.0)
//   - Empty/any:      (no constraint — matches any version)
type NuGetMatcher struct{}

var _ deps.ConstraintParser = NuGetMatcher{}

// ParseConstraint converts a NuGet version range to a PubGrub Condition.
// Returns nil for empty constraints (any version accepted).
func (NuGetMatcher) ParseConstraint(constraint string) pubgrub.Condition {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return nil
	}

	// Try pubgrub's built-in range parser which handles semver ranges including
	// NuGet-style bracket notation: [1.0.0, 2.0.0), [1.0.0], etc.
	vs, err := pubgrub.ParseVersionRange(constraint)
	if err == nil {
		return pubgrub.NewVersionSetCondition(vs)
	}

	// Fall back: treat bare version string as minimum inclusive bound.
	// e.g. "1.2.3" → ">= 1.2.3"
	vs, err = pubgrub.ParseVersionRange(">= " + constraint)
	if err == nil {
		return pubgrub.NewVersionSetCondition(vs)
	}

	return nil
}

// ParseVersion converts a NuGet version string to a PubGrub Version.
func (NuGetMatcher) ParseVersion(version string) pubgrub.Version {
	return pubgrub.SimpleVersion(strings.TrimSpace(version))
}
