package nuget

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/integrations"
)

// PackageDependency holds a NuGet package dependency with its version constraint.
type PackageDependency struct {
	Name       string // Package name (e.g., "Newtonsoft.Json")
	Constraint string // NuGet version range (e.g., "[1.0, 2.0)", "13.0.3"), empty means any
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
	Name          string              // Package name as published (e.g., "Newtonsoft.Json", never empty in valid info)
	Version       string              // Latest version (e.g., "13.0.3", never empty in valid info)
	Dependencies  []PackageDependency // Runtime dependencies with version constraints (nil or empty if none)
	ProjectURL    string              // Project URL from metadata (may be empty)
	RepositoryURL string              // Source repository URL from .nuspec (may be empty)
	Description   string              // Package description (may be empty)
	LicenseURL    string              // License URL (may be empty)
	Authors       string              // Comma-separated author names (may be empty)
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

// NewClient creates a NuGet client with the given cache backend.
//
// Parameters:
//   - backend: Cache backend for HTTP response caching (use cache.NewNullCache() for no caching)
//   - cacheTTL: How long responses are cached (typical: 1-24 hours)
//
// The returned Client is safe for concurrent use.
func NewClient(backend cache.Cache, cacheTTL time.Duration) *Client {
	rl := integrations.DefaultRateLimits["nuget"]
	return &Client{
		Client:          integrations.NewClientWithRateLimit(backend, "nuget:", cacheTTL, nil, rl.RequestsPerSecond, rl.Burst),
		baseURL:         "https://api.nuget.org/v3-flatcontainer",
		registrationURL: "https://api.nuget.org/v3/registration5-semver1",
	}
}

// ListVersions returns all available versions for a package, sorted oldest to newest.
// This implements deps.VersionLister for PubGrub-based dependency resolution.
func (c *Client) ListVersions(ctx context.Context, pkg string, refresh bool) ([]string, error) {
	pkgLower := strings.ToLower(strings.TrimSpace(pkg))
	var versionData versionIndexResponse
	err := c.Cached(ctx, pkgLower+"/versions", refresh, &versionData, func() error {
		versionURL := fmt.Sprintf("%s/%s/index.json", c.baseURL, url.PathEscape(pkgLower))
		return c.Get(ctx, versionURL, &versionData)
	})
	if err != nil {
		return nil, err
	}
	return versionData.Versions, nil
}

// FetchPackageVersion retrieves metadata for a specific version of a .NET package.
// This implements part of deps.Fetcher for PubGrub-based dependency resolution.
func (c *Client) FetchPackageVersion(ctx context.Context, pkg, version string, refresh bool) (*PackageInfo, error) {
	pkgLower := strings.ToLower(strings.TrimSpace(pkg))
	version = strings.TrimSpace(version)

	var info PackageInfo
	err := c.Cached(ctx, pkgLower+"@"+version, refresh, &info, func() error {
		return c.fetchPackageVersion(ctx, pkgLower, version, &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// fetchPackageVersion is the uncached implementation used by FetchPackageVersion.
func (c *Client) fetchPackageVersion(ctx context.Context, pkg, version string, info *PackageInfo) error {
	registrationURL := fmt.Sprintf("%s/%s/%s.json", c.registrationURL, url.PathEscape(pkg), url.PathEscape(version))
	var registrationData registrationResponse
	if err := c.Get(ctx, registrationURL, &registrationData); err != nil {
		if !errors.Is(err, integrations.ErrNotFound) {
			return err
		}
		return c.fetchNuspecMetadata(ctx, pkg, version, info)
	}

	var catalogData catalogEntry
	if registrationData.CatalogEntry != "" {
		if err := c.Get(ctx, registrationData.CatalogEntry, &catalogData); err != nil {
			return c.fetchNuspecMetadata(ctx, pkg, version, info)
		}
	}

	info.Name = catalogData.ID
	if info.Name == "" {
		info.Name = pkg
	}
	info.Version = version
	info.Description = catalogData.Description
	info.ProjectURL = catalogData.ProjectURL
	info.LicenseURL = catalogData.LicenseURL
	info.Authors = catalogData.Authors
	info.Dependencies = extractDependencies(catalogData.DependencyGroups)
	info.RepositoryURL = c.fetchRepositoryURL(ctx, pkg, version)
	return nil
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

	// Prefer latest stable version, fallback to latest pre-release if no stable exists
	latestVersion := findLatestStableVersion(versionData.Versions)

	// Step 2: Get the package metadata from the registration API
	// This returns a reference to the catalog entry
	var registrationData registrationResponse
	registrationURL := fmt.Sprintf("%s/%s/%s.json", c.registrationURL, url.PathEscape(pkg), url.PathEscape(latestVersion))
	if err := c.Get(ctx, registrationURL, &registrationData); err != nil {
		if !errors.Is(err, integrations.ErrNotFound) {
			return err
		}
		// Registration API unavailable (likely pre-release), fall back to .nuspec
		return c.fetchNuspecMetadata(ctx, pkg, latestVersion, info)
	}

	// Step 3: Fetch the actual catalog entry with full metadata
	var catalogData catalogEntry
	if registrationData.CatalogEntry != "" {
		if err := c.Get(ctx, registrationData.CatalogEntry, &catalogData); err != nil {
			// Catalog unavailable (e.g., pre-release version), fall back to .nuspec
			return c.fetchNuspecMetadata(ctx, pkg, latestVersion, info)
		}
	}

	// Populate the PackageInfo from catalog
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

	// Step 4: Fetch .nuspec to get repository URL (not available in catalog entry)
	info.RepositoryURL = c.fetchRepositoryURL(ctx, pkg, latestVersion)

	return nil
}

// fetchRepositoryURL fetches the .nuspec XML for a package and extracts the
// <repository url="..."> element. This is the only reliable source for the
// source repository URL, as the catalog entry JSON does not include it.
//
// Returns an empty string if the .nuspec cannot be fetched or has no repository.
func (c *Client) fetchRepositoryURL(ctx context.Context, pkg, version string) string {
	nuspecURL := fmt.Sprintf("%s/%s/%s/%s.nuspec", c.baseURL, url.PathEscape(pkg), url.PathEscape(version), url.PathEscape(pkg))

	body, err := c.GetText(ctx, nuspecURL)
	if err != nil {
		return ""
	}

	var spec nuspecPackage
	if err := xml.Unmarshal([]byte(body), &spec); err != nil {
		return ""
	}
	return spec.Metadata.Repository.URL
}

// fetchNuspecMetadata fetches the .nuspec XML file and extracts full package metadata.
// This is used as a fallback when the catalog entry is unavailable (e.g., for pre-release versions).
//
// The .nuspec file contains all metadata fields including description, authors, dependencies,
// and repository URL. It's available for all package versions, including pre-releases.
//
// Returns an error if the .nuspec cannot be fetched or parsed.
func (c *Client) fetchNuspecMetadata(ctx context.Context, pkg, version string, info *PackageInfo) error {
	nuspecURL := fmt.Sprintf("%s/%s/%s/%s.nuspec", c.baseURL, url.PathEscape(pkg), url.PathEscape(version), url.PathEscape(pkg))

	body, err := c.GetText(ctx, nuspecURL)
	if err != nil {
		return fmt.Errorf("failed to fetch .nuspec: %w", err)
	}

	var spec nuspecPackage
	if err := xml.Unmarshal([]byte(body), &spec); err != nil {
		return fmt.Errorf("failed to parse .nuspec XML: %w", err)
	}

	// Populate PackageInfo from .nuspec
	info.Name = spec.Metadata.ID
	if info.Name == "" {
		info.Name = pkg
	}
	info.Version = version
	info.Description = spec.Metadata.Description
	info.ProjectURL = spec.Metadata.ProjectURL
	info.LicenseURL = spec.Metadata.LicenseURL
	info.Authors = spec.Metadata.Authors
	info.RepositoryURL = spec.Metadata.Repository.URL

	// Convert .nuspec dependencies to catalog-style dependency groups
	var depGroups []dependencyGroup
	for _, group := range spec.Metadata.Dependencies.Groups {
		var deps []dependency
		for _, dep := range group.Dependencies {
			deps = append(deps, dependency{
				ID:    dep.ID,
				Range: dep.Version,
			})
		}
		depGroups = append(depGroups, dependencyGroup{
			TargetFramework: group.TargetFramework,
			Dependencies:    deps,
		})
	}
	info.Dependencies = extractDependencies(depGroups)

	return nil
}

// findLatestStableVersion returns the latest stable version from a sorted version list.
// Stable versions don't contain hyphens (e.g., "13.0.3" is stable, "11.0.0-preview.1" is pre-release).
//
// Pre-release versions have catalog API limitations (404 on registration endpoint), so we prefer
// stable versions when available. If all versions are pre-release, returns the latest one.
//
// The versions slice is expected to be sorted in ascending order (oldest to newest).
func findLatestStableVersion(versions []string) string {
	// Search backwards from the end (newest) to find first stable version
	for i := len(versions) - 1; i >= 0; i-- {
		if !strings.Contains(versions[i], "-") {
			return versions[i]
		}
	}
	// All versions are pre-release, return the latest one
	return versions[len(versions)-1]
}

// extractDependencies parses dependency groups and returns a flat list of dependencies
// with their version constraints.
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
func extractDependencies(groups []dependencyGroup) []PackageDependency {
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

func dependenciesFromGroup(group dependencyGroup) []PackageDependency {
	if len(group.Dependencies) == 0 {
		return nil
	}
	pkgDeps := make([]PackageDependency, 0, len(group.Dependencies))
	for _, dep := range group.Dependencies {
		pkgDeps = append(pkgDeps, PackageDependency{Name: dep.ID, Constraint: dep.Range})
	}
	return pkgDeps
}

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
			n1, _ = strconv.Atoi(parts1[i])
		}
		if i < len(parts2) {
			n2, _ = strconv.Atoi(parts2[i])
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

// .nuspec XML structures for extracting package metadata

type nuspecPackage struct {
	XMLName  xml.Name       `xml:"package"`
	Metadata nuspecMetadata `xml:"metadata"`
}

type nuspecMetadata struct {
	ID           string             `xml:"id"`
	Version      string             `xml:"version"`
	Description  string             `xml:"description"`
	Authors      string             `xml:"authors"`
	ProjectURL   string             `xml:"projectUrl"`
	LicenseURL   string             `xml:"licenseUrl"`
	Repository   nuspecRepository   `xml:"repository"`
	Dependencies nuspecDependencies `xml:"dependencies"`
}

type nuspecRepository struct {
	Type string `xml:"type,attr"`
	URL  string `xml:"url,attr"`
}

type nuspecDependencies struct {
	Groups []nuspecDependencyGroup `xml:"group"`
}

type nuspecDependencyGroup struct {
	TargetFramework string             `xml:"targetFramework,attr"`
	Dependencies    []nuspecDependency `xml:"dependency"`
}

type nuspecDependency struct {
	ID      string `xml:"id,attr"`
	Version string `xml:"version,attr"`
}
