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

// modernNetRegex matches modern .NET target frameworks:
// - netstandard (any version)
// - netcoreapp (any version)
// - net5.0, net6.0, net7.0, net8.0, net9.0, net10.0+ (unified .NET 5+)
// The regex requires a period after the version number to distinguish modern .NET (net6.0)
// from legacy .NET Framework (net47, net481).
// Examples: ".NETStandard2.0", "netcoreapp3.1", "net6.0", "net10.0", "net7.0-windows"
var modernNetRegex = regexp.MustCompile(`(?i)net(standard|coreapp|\d+\.)`)

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
// This function implements a three-stage selection strategy to find the most compatible
// dependency set:
//
//  1. Framework-agnostic dependencies (empty or "any" targetFramework)
//  2. .NET Standard/Core/5+ dependencies (netstandard*, netcoreapp*, net6-8)
//  3. First available dependency group as fallback
//
// This prioritizes modern .NET dependencies over legacy .NET Framework dependencies.
func extractDependencies(groups []dependencyGroup) []string {
	if len(groups) == 0 {
		return nil
	}

	var deps []string

	// Stage 1: Look for framework-agnostic dependencies
	// These dependencies apply to all target frameworks and are the safest choice
	for _, group := range groups {
		if group.TargetFramework == "" || group.TargetFramework == "any" {
			for _, dep := range group.Dependencies {
				deps = append(deps, dep.ID)
			}
			return deps
		}
	}

	// Stage 2: Prefer .NET Standard, .NET Core, or modern .NET dependencies
	// Modern .NET frameworks (netstandard, netcoreapp, net5+) provide the best compatibility
	// Uses regex to match any current or future .NET version (net5, net6, net7, ... net99+)
	for _, group := range groups {
		if modernNetRegex.MatchString(group.TargetFramework) {
			for _, dep := range group.Dependencies {
				deps = append(deps, dep.ID)
			}
			return deps
		}
	}

	// Stage 3: Fallback to first available group
	// If no framework-agnostic or modern .NET dependencies exist, use the first group
	// This handles legacy .NET Framework packages and edge cases
	if len(groups) > 0 && len(groups[0].Dependencies) > 0 {
		for _, dep := range groups[0].Dependencies {
			deps = append(deps, dep.ID)
		}
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
