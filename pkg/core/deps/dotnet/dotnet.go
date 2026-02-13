package dotnet

import (
	"context"
	"time"

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

func newResolver(backend cache.Cache, ttl time.Duration) (deps.Resolver, error) {
	c := nuget.NewClient(backend, ttl)
	return deps.NewRegistry("nuget", fetcher{c}), nil
}

type fetcher struct{ *nuget.Client }

func (f fetcher) Fetch(ctx context.Context, name string, refresh bool) (*deps.Package, error) {
	p, err := f.FetchPackage(ctx, name, refresh)
	if err != nil {
		return nil, err
	}
	return &deps.Package{
		Name:         p.Name,
		Version:      p.Version,
		Dependencies: p.Dependencies,
		Description:  p.Description,
		License:      p.LicenseURL,
		Author:       p.Authors,
		Repository:   p.RepositoryURL,
		HomePage:     p.ProjectURL,
		ManifestFile: "*.csproj",
	}, nil
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
