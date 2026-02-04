// Package nuget provides an HTTP client for the NuGet.org package registry API.
//
// # Overview
//
// This package fetches package metadata from NuGet.org
// (https://api.nuget.org/v3), the package manager for .NET.
//
// # Usage
//
//	client := nuget.NewClient(cache.NewNullCache(), 24*time.Hour)
//
//	pkg, err := client.FetchPackage(ctx, "Newtonsoft.Json", false)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Println(pkg.Name, pkg.Version)
//	fmt.Println("Dependencies:", pkg.Dependencies)
//
// # PackageInfo
//
// [FetchPackage] returns a [PackageInfo] containing:
//
//   - Name, Version: Package identity (latest stable or prerelease version)
//   - Dependencies: Runtime dependencies from the most compatible target framework
//   - Description: Package description
//   - LicenseURL, Authors: Package metadata
//   - ProjectURL: Homepage URL for enrichment
//
// # Caching
//
// Responses are cached to reduce load on the registry. The cache TTL is set
// when creating the client. Pass refresh=true to bypass the cache.
//
// # Version Selection
//
// The client fetches the latest version from the flatcontainer index.
// Versions are typically sorted by the API, with the last entry being the most recent.
// Prerelease versions are included if no stable version exists.
//
// # Dependency Filtering
//
// NuGet packages can have different dependencies for different target frameworks.
// The client prioritizes dependencies in this order:
//
//  1. Framework-agnostic dependencies (no target framework specified)
//  2. .NET Standard/Core dependencies (netstandard*, netcoreapp*, net6.0+)
//  3. First available dependency group as fallback
//
// This strategy favors modern .NET dependencies over legacy .NET Framework dependencies.
//
// # Package Name Normalization
//
// NuGet package names are case-insensitive. Package names are normalized to
// lowercase for API calls but preserve their original casing in PackageInfo.
package nuget
