// Package nuget provides a client for the NuGet.org package registry API.
//
// It implements the integrations.Client pattern for fetching .NET package metadata
// including dependencies, versioning, and project information.
//
// Example usage:
//
//	client, err := nuget.NewClient(24 * time.Hour)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	pkg, err := client.FetchPackage(context.Background(), "Newtonsoft.Json", false)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Printf("Package: %s v%s\n", pkg.Name, pkg.Version)
//	fmt.Printf("Dependencies: %v\n", pkg.Dependencies)
package nuget
