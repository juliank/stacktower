package dotnet

import (
	"context"
	"fmt"
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
)

// DirectoryPackagesProps parses Directory.Packages.props files (Central Package Management).
// It treats PackageVersion entries as direct dependencies.
type DirectoryPackagesProps struct {
	resolver deps.Resolver
}

// Type returns the manifest type identifier.
func (p *DirectoryPackagesProps) Type() string {
	return "cpm"
}

// IncludesTransitive returns true if a resolver is available to fetch transitive dependencies.
func (p *DirectoryPackagesProps) IncludesTransitive() bool {
	return p.resolver != nil
}

// Supports checks if the filename matches this parser.
func (p *DirectoryPackagesProps) Supports(name string) bool {
	return strings.EqualFold(name, "Directory.Packages.props")
}

// Parse reads a Directory.Packages.props file and builds a dependency graph.
// It only includes direct dependencies; transitive dependencies must be resolved via the registry.
func (p *DirectoryPackagesProps) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	entries, err := parseCPMProps(path)
	if err != nil {
		return nil, err
	}

	g := dag.New(nil)
	g.AddNode(dag.Node{ID: projectRoot, Row: 0})

	directDeps := cpmPackageIDs(entries)

	if p.resolver != nil {
		resolvedGraph, err := p.resolve(context.Background(), directDeps, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve dependencies: %w", err)
		}
		g = resolvedGraph
	} else {
		addCPMPackages(g, entries)
	}

	return &deps.ManifestResult{
		Graph:              g,
		Type:               p.Type(),
		IncludesTransitive: p.IncludesTransitive(),
		RootPackage:        rootPackageFromPath(path),
	}, nil
}

func (p *DirectoryPackagesProps) resolve(ctx context.Context, pkgs []deps.Dependency, opts deps.Options) (*dag.DAG, error) {
	return resolveTransitive(ctx, p.resolver, pkgs, opts)
}

func cpmPackageIDs(entries []cpmPackageVersion) []deps.Dependency {
	ids := make([]deps.Dependency, 0, len(entries))
	for _, entry := range entries {
		if entry.Include != "" {
			d := deps.DependencyFromName(entry.Include)
			if entry.Version != "" {
				d.Pinned = entry.Version
			}
			ids = append(ids, d)
		}
	}
	return ids
}

func addCPMPackages(g *dag.DAG, entries []cpmPackageVersion) {
	for _, entry := range entries {
		if entry.Include == "" {
			continue
		}
		meta := dag.Metadata{"version": entry.Version}
		g.AddNode(dag.Node{ID: entry.Include, Meta: meta})
		g.AddEdge(dag.Edge{From: projectRoot, To: entry.Include})
	}
}
