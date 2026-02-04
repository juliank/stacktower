package nuget

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/matzehuels/stacktower/pkg/integrations"
)

// frameworkRegex extracts framework family and version from target framework strings.
// Captures: family (net/netstandard/netcoreapp) and version (digits and periods).
// Examples: "net6.0" → ("net", "6.0"), "netstandard2.1" → ("netstandard", "2.1")
var frameworkRegex = regexp.MustCompile(`(?i)^(net)(standard|coreapp)?(\d+(?:\.\d+)*)`)

// targetFramework represents a parsed .NET target framework.
type targetFramework struct {
	family  string // "net", "netstandard", "netcoreapp", "netframework"
	version string // e.g., "6.0", "2.1", "3.1", "481"
	raw     string // original string for debugging
}

// frameworkPriority returns a numeric priority for framework families.
// Higher values represent newer/preferred frameworks.
func frameworkPriority(family string) int {
	switch family {
	case "net":
		return 300 // Modern .NET (net5.0+)
	case "netcoreapp":
		return 200 // .NET Core
	case "netstandard":
		return 100 // .NET Standard
	case "netframework":
		return 10 // Legacy .NET Framework (net481, net48, etc.)
	default:
		return 0 // Unknown
	}
}

// parseTargetFramework parses a target framework string into its components.
// Returns nil if the string doesn't match a recognized .NET framework pattern.
func parseTargetFramework(tf string) *targetFramework {
	tf = strings.ToLower(strings.TrimSpace(tf))
	if tf == "" || tf == "any" {
		return nil
	}

	matches := frameworkRegex.FindStringSubmatch(tf)
	if len(matches) < 4 {
		return nil
	}

	family := "net"
	version := matches[3]

	if matches[2] != "" {
		family = "net" + matches[2] // "netstandard" or "netcoreapp"
	} else if version != "" && !strings.Contains(version, ".") {
		// Legacy .NET Framework: net481, net48, net472 (no period in version)
		family = "netframework"
	}
	// else: Modern .NET with period: net6.0, net8.0

	return &targetFramework{
		family:  family,
		version: matches[3],
		raw:     tf,
	}
}

// compareVersions compares two version strings numerically.
// Returns: 1 if v1 > v2, -1 if v1 < v2, 0 if equal
// Handles multi-part versions like "6.0", "2.1.1", etc.
func compareVersions(v1, v2 string) int {
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(parts1) {
			fmt.Sscanf(parts1[i], "%d", &n1)
		}
		if i < len(parts2) {
			fmt.Sscanf(parts2[i], "%d", &n2)
		}
		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}
	return 0
}

// isNewerFramework returns true if tf1 is newer/more preferred than tf2.
// Comparison logic:
//  1. Compare family priority (modern .NET > .NET Core > .NET Standard)
//  2. If same family, compare version numbers
func isNewerFramework(tf1, tf2 *targetFramework) bool {
	if tf1 == nil {
		return false
	}
	if tf2 == nil {
		return true
	}

	p1 := frameworkPriority(tf1.family)
	p2 := frameworkPriority(tf2.family)

	if p1 != p2 {
		return p1 > p2
	}

	// Same family, compare versions
	return compareVersions(tf1.version, tf2.version) > 0
}

// PackageInfo holds metadata for a .NET package from NuGet.org.
//
// Package names in NuGet are case-insensitive but preserve their original casing.
// The Version field contains the latest stable version (or latest prerelease if no stable exists).
// Dependencies include only runtime dependencies from the most compatible target framework.
//
// Zero values: All string fields are empty, Dependencies is nil.
// This struct is safe for concurrent reads after construction.
type PackageInfo struct {
	Name         string   // Package name as published (e.g., "Newtonsoft.Json", never empty in valid info)
	Version      string   // Latest version (e.g., "13.0.3", never empty in valid info)
	Dependencies []string // Runtime dependency names (nil or empty if none)
	ProjectURL   string   // Project URL from metadata (may be empty)
	Description  string   // Package description (may be empty)
	LicenseURL   string   // License URL (may be empty)
	Authors      string   // Comma-separated author names (may be empty)
}

// Client provides access to the NuGet.org package registry API.
// It handles HTTP requests with caching and automatic retries.
//
// All methods are safe for concurrent use by multiple goroutines.
type Client struct {
	*integrations.Client
	baseURL         string
	registrationURL string
}

// NewClient creates a NuGet client with the specified cache TTL.
//
// The cacheTTL parameter sets how long responses are cached.
// Typical values: 1-24 hours for production, 0 for testing (no cache).
//
// Returns an error if the cache directory cannot be created or accessed.
// The returned Client is safe for concurrent use.
func NewClient(cacheTTL time.Duration) (*Client, error) {
	cache, err := integrations.NewCacheWithNamespace("nuget:", cacheTTL)
	if err != nil {
		return nil, err
	}
	return &Client{
		Client:          integrations.NewClient(cache, nil),
		baseURL:         "https://api.nuget.org/v3-flatcontainer",
		registrationURL: "https://api.nuget.org/v3/registration5-semver1",
	}, nil
}

// FetchPackage retrieves metadata for a .NET package from NuGet.org.
//
// The pkg parameter is normalized to lowercase for API requests (NuGet is case-insensitive).
// Package name cannot be empty; an empty string will result in an API error.
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
// If refresh is false, cached data is returned if available and not expired.
//
// Returns:
//   - PackageInfo populated with metadata for the latest version
//   - [integrations.ErrNotFound] if the package doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//   - Other errors for JSON decoding failures
//
// The returned PackageInfo pointer is never nil if err is nil.
// This method is safe for concurrent use.
func (c *Client) FetchPackage(ctx context.Context, pkg string, refresh bool) (*PackageInfo, error) {
	// NuGet package names are case-insensitive, so we normalize to lowercase for API calls
	pkgLower := strings.ToLower(strings.TrimSpace(pkg))
	if pkgLower == "" {
		return nil, fmt.Errorf("package name cannot be empty")
	}
	key := pkgLower

	var info PackageInfo
	err := c.Cached(ctx, key, refresh, &info, func() error {
		return c.fetch(ctx, pkgLower, &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) fetch(ctx context.Context, pkg string, info *PackageInfo) error {
	// Step 1: Get the version list to find the latest version
	var versionData versionIndexResponse
	versionURL := fmt.Sprintf("%s/%s/index.json", c.baseURL, url.PathEscape(pkg))
	if err := c.Get(ctx, versionURL, &versionData); err != nil {
		if errors.Is(err, integrations.ErrNotFound) {
			return fmt.Errorf("%w: nuget package %s", err, pkg)
		}
		return err
	}

	if len(versionData.Versions) == 0 {
		return fmt.Errorf("no versions found for package %s (API returned empty version list)", pkg)
	}

	// Get the latest version (versions are typically sorted, but we take the last one)
	latestVersion := versionData.Versions[len(versionData.Versions)-1]

	// Step 2: Get the package metadata from the registration API
	// This returns a reference to the catalog entry
	var registrationData registrationResponse
	registrationURL := fmt.Sprintf("%s/%s/%s.json", c.registrationURL, url.PathEscape(pkg), url.PathEscape(latestVersion))
	if err := c.Get(ctx, registrationURL, &registrationData); err != nil {
		if !errors.Is(err, integrations.ErrNotFound) {
			return err
		}
		// If we can't get metadata, return basic info
		info.Name = pkg
		info.Version = latestVersion
		return nil
	}

	// Step 3: Fetch the actual catalog entry with full metadata
	var catalogData catalogEntry
	if registrationData.CatalogEntry != "" {
		if err := c.Get(ctx, registrationData.CatalogEntry, &catalogData); err != nil {
			// If catalog fetch fails, use basic info
			info.Name = pkg
			info.Version = latestVersion
			return nil
		}
	}

	// Populate the PackageInfo
	info.Name = catalogData.ID
	if info.Name == "" {
		info.Name = pkg
	}
	info.Version = latestVersion
	info.Description = catalogData.Description
	info.ProjectURL = catalogData.ProjectURL
	info.LicenseURL = catalogData.LicenseURL
	info.Authors = catalogData.Authors

	// Extract dependencies
	info.Dependencies = extractDependencies(catalogData.DependencyGroups)

	return nil
}

// extractDependencies parses dependency groups and returns a flat list of dependency names.
//
// NuGet packages can have different dependencies for different target frameworks.
// This function selects the dependency group for the newest/most modern target framework
// available. Framework ordering (newest first):
//   - Modern .NET: net9.0 > net8.0 > net7.0 > net6.0 > net5.0
//   - .NET Core: netcoreapp3.1 > netcoreapp3.0 > netcoreapp2.2 > ...
//   - .NET Standard: netstandard2.1 > netstandard2.0 > netstandard1.6 > ...
//   - Legacy .NET Framework: net481 > net48 > net472 > ...
//
// Framework-agnostic dependencies (empty or "any" targetFramework) are returned immediately
// as they apply to all frameworks.
func extractDependencies(groups []dependencyGroup) []string {
	if len(groups) == 0 {
		return nil
	}

	// Stage 1: Look for framework-agnostic dependencies
	// These dependencies apply to all target frameworks and are the safest choice
	for _, group := range groups {
		if group.TargetFramework == "" || group.TargetFramework == "any" {
			return dependenciesFromGroup(group)
		}
	}

	// Stage 2: Find the newest target framework among all groups
	var newestGroup *dependencyGroup
	var newestFramework *targetFramework

	for i := range groups {
		tf := parseTargetFramework(groups[i].TargetFramework)
		if tf == nil {
			continue
		}

		if isNewerFramework(tf, newestFramework) {
			newestFramework = tf
			newestGroup = &groups[i]
		}
	}

	// Return dependencies from the newest framework group
	if newestGroup != nil {
		return dependenciesFromGroup(*newestGroup)
	}

	// Stage 3: Fallback to first available group if no frameworks were parseable
	if len(groups) > 0 && len(groups[0].Dependencies) > 0 {
		return dependenciesFromGroup(groups[0])
	}

	return nil
}

func dependenciesFromGroup(group dependencyGroup) []string {
	if len(group.Dependencies) == 0 {
		return nil
	}
	deps := make([]string, 0, len(group.Dependencies))
	for _, dep := range group.Dependencies {
		deps = append(deps, dep.ID)
	}
	return deps
}

// API response structures for NuGet.org JSON API

type versionIndexResponse struct {
	Versions []string `json:"versions"`
}

type registrationResponse struct {
	CatalogEntry string `json:"catalogEntry"` // URL to the catalog entry
}

type catalogEntry struct {
	ID               string            `json:"id"`
	Version          string            `json:"version"`
	Description      string            `json:"description"`
	Authors          string            `json:"authors"`
	ProjectURL       string            `json:"projectUrl"`
	LicenseURL       string            `json:"licenseUrl"`
	DependencyGroups []dependencyGroup `json:"dependencyGroups"`
}

type dependencyGroup struct {
	TargetFramework string       `json:"targetFramework"`
	Dependencies    []dependency `json:"dependencies"`
}

type dependency struct {
	ID    string `json:"id"`
	Range string `json:"range"`
}
