package dotnet

import (
	"context"
	"strings"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/integrations/nuget"
)

// Language provides .NET dependency resolution via NuGet.org.
// Supports packages.config and .csproj manifest files.
var Language = &deps.Language{
	Name:            "dotnet",
	DefaultRegistry: "nuget",
	ManifestTypes:   []string{"packages", "csproj", "cpm"},
	ManifestAliases: map[string]string{
		"packages.config":          "packages",
		"*.csproj":                 "csproj",
		"directory.packages.props": "cpm",
	},
	NewResolver:     newResolver,
	NewManifest:     newManifest,
	ManifestParsers: manifestParsers,
}

func newResolver(backend cache.Cache, opts deps.Options) (deps.Resolver, error) {
	c := nuget.NewClient(backend, opts.CacheTTL)
	return deps.NewPubGrubResolver("nuget", fetcher{c}, NuGetMatcher{})
}

type fetcher struct{ *nuget.Client }

func (f fetcher) Fetch(ctx context.Context, name string, refresh bool) (*deps.Package, error) {
	p, err := f.FetchPackage(ctx, name, refresh)
	if err != nil {
		return nil, err
	}
	return nugetPkgToDepsPkg(p), nil
}

func (f fetcher) FetchVersion(ctx context.Context, name, version string, refresh bool) (*deps.Package, error) {
	p, err := f.Client.FetchPackageVersion(ctx, name, version, refresh)
	if err != nil {
		return nil, err
	}
	return nugetPkgToDepsPkg(p), nil
}

func (f fetcher) ListVersions(ctx context.Context, name string, refresh bool) ([]string, error) {
	return f.Client.ListVersions(ctx, name, refresh)
}

func nugetPkgToDepsPkg(p *nuget.PackageInfo) *deps.Package {
	pkg := &deps.Package{
		Name:         p.Name,
		Version:      p.Version,
		Description:  p.Description,
		License:      p.LicenseURL,
		Author:       p.Authors,
		Repository:   p.RepositoryURL,
		HomePage:     p.ProjectURL,
		ManifestFile: "*.csproj",
	}
	for _, name := range p.Dependencies {
		pkg.Dependencies = append(pkg.Dependencies, deps.DependencyFromName(name))
	}
	return pkg
}

func newManifest(name string, res deps.Resolver) deps.ManifestParser {
	switch name {
	case "packages":
		return &PackagesConfig{resolver: res}
	case "csproj":
		return &CsProj{resolver: res}
	case "cpm":
		return &DirectoryPackagesProps{resolver: res}
	default:
		nameLower := strings.ToLower(name)
		// .csproj files have variable names (e.g., "MyProject.csproj")
		if strings.HasSuffix(nameLower, ".csproj") {
			return &CsProj{resolver: res}
		}
		// packages.config and Directory.Packages.props are case-insensitive on Windows
		if nameLower == "packages.config" {
			return &PackagesConfig{resolver: res}
		}
		if nameLower == "directory.packages.props" {
			return &DirectoryPackagesProps{resolver: res}
		}
		return nil
	}
}

func manifestParsers(res deps.Resolver) []deps.ManifestParser {
	return []deps.ManifestParser{
		&PackagesConfig{resolver: res},
		&CsProj{resolver: res},
		&DirectoryPackagesProps{resolver: res},
	}
}
