package dotnet

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/contriboss/pubgrub-go"

	"github.com/stacktower-io/stacktower/pkg/core/deps"
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

	// NuGet uses interval/bracket notation which pubgrub does not understand natively.
	// Convert to semver range syntax before passing to pubgrub.
	// Examples: "[4.0.0, )" → ">= 4.0.0", "[1.0, 2.0)" → ">= 1.0 < 2.0", "[1.0.0]" → "= 1.0.0"
	if converted, ok := nugetRangeToSemver(constraint); ok {
		vs, err := pubgrub.ParseVersionRange(converted)
		if err == nil {
			return pubgrub.NewVersionSetCondition(vs)
		}
	}

	// Fall back: treat bare version string as minimum inclusive bound.
	// e.g. "1.2.3" → ">= 1.2.3"
	vs, err := pubgrub.ParseVersionRange(">= " + constraint)
	if err == nil {
		return pubgrub.NewVersionSetCondition(vs)
	}

	slog.Warn("nuget: unrecognized version constraint, treating as unconstrained", "constraint", constraint)
	return nil
}

// nugetRangeToSemver converts NuGet interval notation to a semver range string
// that pubgrub can parse. Returns (converted, true) on success.
//
// NuGet interval notation:
//
//	[min, max]  → >= min <= max
//	[min, max)  → >= min < max
//	(min, max]  → > min <= max
//	(min, max)  → > min < max
//	[min, )     → >= min
//	(min, )     → > min
//	(, max]     → <= max
//	(, max)     → < max
//	[exact]     → = exact
func nugetRangeToSemver(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return "", false
	}

	openInclusive := s[0] == '['
	closeInclusive := s[len(s)-1] == ']'

	if s[0] != '[' && s[0] != '(' {
		return "", false
	}
	if s[len(s)-1] != ']' && s[len(s)-1] != ')' {
		return "", false
	}

	inner := strings.TrimSpace(s[1 : len(s)-1])

	// Exact version: [1.0.0]
	if !strings.Contains(inner, ",") {
		if openInclusive && closeInclusive {
			return "= " + strings.TrimSpace(inner), true
		}
		return "", false
	}

	parts := strings.SplitN(inner, ",", 2)
	minVer := strings.TrimSpace(parts[0])
	maxVer := strings.TrimSpace(parts[1])

	var result []string
	if minVer != "" {
		if openInclusive {
			result = append(result, ">= "+minVer)
		} else {
			result = append(result, "> "+minVer)
		}
	}
	if maxVer != "" {
		if closeInclusive {
			result = append(result, "<= "+maxVer)
		} else {
			result = append(result, "< "+maxVer)
		}
	}

	if len(result) == 0 {
		return "", false
	}
	return strings.Join(result, " "), true
}

// nugetVersionRE matches NuGet version strings: major.minor.patch[.revision][-prerelease][+build]
var nugetVersionRE = regexp.MustCompile(`^(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:\.(\d+))?(?:-([\w.\-]+))?(?:\+[\w.]+)?$`)

// nugetVersion holds a parsed NuGet version.
type nugetVersion struct {
	major      int
	minor      int
	patch      int
	prerelease string
	valid      bool
}

func parseNuGetVersion(v string) nugetVersion {
	nv := nugetVersion{}
	v = strings.TrimSpace(v)
	m := nugetVersionRE.FindStringSubmatch(v)
	if m == nil {
		return nv
	}
	nv.valid = true
	nv.major, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		nv.minor, _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		nv.patch, _ = strconv.Atoi(m[3])
	}
	// m[4] is the optional 4th revision segment — ignored for semver ordering
	nv.prerelease = m[5]
	return nv
}

// ParseVersion converts a NuGet version string to a PubGrub SemanticVersion.
// SemanticVersion is used (instead of SimpleVersion) so that multi-digit version
// components sort numerically — e.g. 10.0.0 > 9.0.0 — matching NuGet ordering.
func (NuGetMatcher) ParseVersion(version string) pubgrub.Version {
	nv := parseNuGetVersion(version)
	if !nv.valid {
		return nil
	}
	if nv.prerelease != "" {
		return pubgrub.NewSemanticVersionWithPrerelease(nv.major, nv.minor, nv.patch, nv.prerelease)
	}
	sv, err := pubgrub.ParseSemanticVersion(fmt.Sprintf("%d.%d.%d", nv.major, nv.minor, nv.patch))
	if err != nil {
		return pubgrub.SimpleVersion(strings.TrimSpace(version))
	}
	return sv
}
