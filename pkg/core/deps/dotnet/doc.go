// Package dotnet provides dependency resolution for .NET packages.
//
// # Overview
//
// This package implements [deps.Language] for .NET, supporting:
//
//   - NuGet.org registry resolution via [nuget] client
//   - .csproj manifest parsing (SDK-style, .NET Core/.NET 5+)
//   - packages.config parsing (legacy .NET Framework format)
//   - Central Package Management (Directory.Packages.props)
//   - ProjectReference support with recursive parsing
//
// # Registry Resolution
//
// Use [Language.Resolver] to fetch dependencies from NuGet.org:
//
//	resolver, _ := dotnet.Language.Resolver()
//	g, _ := resolver.Resolve(ctx, "Newtonsoft.Json", deps.Options{MaxDepth: 10})
//
// # Manifest Parsing
//
// Parse local manifest files:
//
//	parser, _ := dotnet.Language.Manifest("csproj", nil)
//	result, _ := parser.Parse("MyProject.csproj", deps.Options{})
//
// Supported manifests:
//
//   - .csproj: SDK-style projects with PackageReference and ProjectReference (IncludesTransitive: true with resolver)
//   - packages.config: Legacy format with package elements (IncludesTransitive: true with resolver)
//   - Directory.Packages.props: CPM version list treated as direct dependencies (IncludesTransitive: true with resolver)
//
// # Central Package Management
//
// SDK-style .csproj files can use Central Package Management (CPM) where versions
// are defined in Directory.Packages.props. The parser automatically searches
// parent directories for this file and resolves versions accordingly.
//
// # ProjectReference Support
//
// The .csproj parser recursively follows ProjectReference elements to include
// dependencies from referenced projects. Each referenced project is parsed
// independently, and its dependencies are merged into the main dependency graph.
//
// # Package Name Normalization
//
// NuGet package names are case-insensitive. Package names are normalized to
// lowercase for API requests but preserve their original casing in results.
//
// [nuget]: github.com/matzehuels/stacktower/pkg/integrations/nuget
// [deps.Language]: github.com/matzehuels/stacktower/pkg/core/deps.Language
package dotnet
