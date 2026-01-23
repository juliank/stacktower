// Package dotnet provides .NET dependency resolution via NuGet.org.
//
// It implements dependency resolution for .NET projects using the NuGet package registry.
// Supports parsing packages.config, .csproj, and other .NET manifest formats.
//
// Example usage:
//
//	// Register the language
//	deps.RegisterLanguage(dotnet.Language)
//
//	// Create a resolver
//	resolver, err := dotnet.Language.NewResolver(24 * time.Hour)
//	if err != nil {
//	    log.Fatal(err)
//	}
package dotnet
