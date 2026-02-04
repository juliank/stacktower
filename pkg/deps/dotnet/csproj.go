package dotnet

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/matzehuels/stacktower/pkg/dag"
	"github.com/matzehuels/stacktower/pkg/deps"
)

// CsProj parses .csproj files (modern SDK-style .NET Core/.NET 5+ format).
// Format: MSBuild XML file with PackageReference elements.
// Supports Central Package Management (CPM) by looking for Directory.Packages.props.
//
// Example .csproj:
//
//	<Project Sdk="Microsoft.NET.Sdk">
//	  <PropertyGroup>
//	    <TargetFramework>net8.0</TargetFramework>
//	  </PropertyGroup>
//	  <ItemGroup>
//	    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
//	    <PackageReference Include="Microsoft.Extensions.Logging" Version="8.0.0" />
//	  </ItemGroup>
//	</Project>
//
// With CPM, versions are omitted and centrally managed:
//
//	Directory.Packages.props:
//	<Project>
//	  <ItemGroup>
//	    <PackageVersion Include="Newtonsoft.Json" Version="13.0.3" />
//	  </ItemGroup>
//	</Project>
type CsProj struct {
	resolver deps.Resolver
}

// Type returns the manifest type identifier.
func (p *CsProj) Type() string {
	return "csproj"
}

// IncludesTransitive returns true if a resolver is available to fetch transitive dependencies.
func (p *CsProj) IncludesTransitive() bool {
	return p.resolver != nil
}

// Supports checks if the filename matches this parser.
func (p *CsProj) Supports(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".csproj")
}

// Parse reads a .csproj file and builds a dependency graph.
// It only includes direct dependencies; transitive dependencies must be resolved via the registry.
// Supports Central Package Management (CPM) by searching for Directory.Packages.props in parent directories.
func (p *CsProj) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	// Apply defaults to options (including no-op logger if nil)
	opts = opts.WithDefaults()

	// Read the XML file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read .csproj: %w", err)
	}

	// Parse XML structure
	var project csprojXML
	if err := xml.Unmarshal(data, &project); err != nil {
		return nil, fmt.Errorf("failed to parse .csproj XML: %w", err)
	}

	// Try to load Central Package Management versions
	cpmVersions, err := loadCPMVersions(path)
	if err != nil {
		// CPM is optional, continue without it
		opts.Logger("Central Package Management not loaded: %v", err)
		cpmVersions = nil
	}

	// Create graph
	g := dag.New(nil)

	// Add root node (project) - use placeholder
	rootID := projectRoot
	g.AddNode(dag.Node{ID: rootID, Row: 0})

	// Try to infer project name from filename
	projectName := projectNameFromPath(path)

	// Collect direct package dependencies
	directDeps := collectDirectDeps(project)

	// Process project references recursively
	baseDir := filepath.Dir(path)
	p.mergeProjectReferences(g, project, baseDir, rootID, opts)

	// If resolver is available, fetch transitive dependencies
	if p.resolver != nil {
		var err error
		resolvedGraph, err := p.resolve(context.Background(), directDeps, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve dependencies: %w", err)
		}

		// Merge project references into the resolved graph
		for _, n := range g.Nodes() {
			if n.ID != projectRoot {
				resolvedGraph.AddNode(*n)
			}
		}
		for _, e := range g.Edges() {
			if e.From == projectRoot {
				// Reconnect from root
				resolvedGraph.AddEdge(dag.Edge{From: projectRoot, To: e.To})
			} else {
				resolvedGraph.AddEdge(e)
			}
		}

		g = resolvedGraph
	} else {
		// Without resolver, just add direct dependencies
		addDirectDependencies(g, project, cpmVersions, rootID)
	}

	return &deps.ManifestResult{
		Graph:              g,
		Type:               p.Type(),
		IncludesTransitive: p.IncludesTransitive(),
		RootPackage:        projectName,
	}, nil
}

// XML structure for .csproj files
type csprojXML struct {
	XMLName    xml.Name          `xml:"Project"`
	ItemGroups []csprojItemGroup `xml:"ItemGroup"`
}

type csprojItemGroup struct {
	PackageReferences []csprojPackageReference `xml:"PackageReference"`
	ProjectReferences []csprojProjectReference `xml:"ProjectReference"`
}

type csprojPackageReference struct {
	Include string `xml:"Include,attr"`
	Version string `xml:"Version,attr"`
}

type csprojProjectReference struct {
	Include string `xml:"Include,attr"`
}

func collectDirectDeps(project csprojXML) []string {
	var directDeps []string
	for _, itemGroup := range project.ItemGroups {
		for _, pkg := range itemGroup.PackageReferences {
			if pkg.Include != "" {
				directDeps = append(directDeps, pkg.Include)
			}
		}
	}
	return directDeps
}

func projectNameFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".csproj")
}

func (p *CsProj) mergeProjectReferences(g *dag.DAG, project csprojXML, baseDir, rootID string, opts deps.Options) {
	for _, itemGroup := range project.ItemGroups {
		for _, proj := range itemGroup.ProjectReferences {
			if proj.Include == "" {
				continue
			}

			// Resolve relative path (normalize Windows backslashes)
			projPath := strings.ReplaceAll(proj.Include, "\\", "/")
			referencedPath := filepath.Join(baseDir, projPath)
			referencedPath = filepath.Clean(referencedPath)

			// Recursively parse the referenced project
			refResult, err := p.Parse(referencedPath, opts)
			if err != nil {
				opts.Logger("failed to parse project reference %s: %v", referencedPath, err)
				continue
			}

			// Extract project name for the node
			projName := projectNameFromPath(referencedPath)

			// Add project node and edge
			g.AddNode(dag.Node{ID: projName})
			g.AddEdge(dag.Edge{From: rootID, To: projName})

			// Merge the referenced project's dependencies into our graph
			refGraph := refResult.Graph.(*dag.DAG)
			for _, n := range refGraph.Nodes() {
				if n.ID != projectRoot {
					g.AddNode(*n)
				}
			}
			for _, e := range refGraph.Edges() {
				if e.From == projectRoot {
					// Redirect edges from referenced project's root to the project node
					g.AddEdge(dag.Edge{From: projName, To: e.To})
				} else {
					g.AddEdge(e)
				}
			}
		}
	}
}

func addDirectDependencies(g *dag.DAG, project csprojXML, cpmVersions map[string]string, rootID string) {
	for _, itemGroup := range project.ItemGroups {
		for _, pkg := range itemGroup.PackageReferences {
			if pkg.Include == "" {
				continue
			}

			// If version is not specified in .csproj, try CPM (case-insensitive lookup)
			version := pkg.Version
			if version == "" && cpmVersions != nil {
				if cpmVer, ok := cpmVersions[strings.ToLower(pkg.Include)]; ok {
					version = cpmVer
				}
			}

			// Create node with version metadata if available
			meta := dag.Metadata{}
			if version != "" {
				meta["version"] = version
			}
			g.AddNode(dag.Node{ID: pkg.Include, Meta: meta})
			g.AddEdge(dag.Edge{From: rootID, To: pkg.Include})
		}
	}
}

// loadCPMVersions searches for Directory.Packages.props in parent directories
// and returns a map of package name to version for Central Package Management.
func loadCPMVersions(csprojPath string) (map[string]string, error) {
	// Start from the .csproj directory and walk up
	dir := filepath.Dir(csprojPath)

	for {
		propsPath := filepath.Join(dir, "Directory.Packages.props")

		// Check if the file exists
		if _, err := os.Stat(propsPath); err == nil {
			// Found it, parse it
			return parseCPMFile(propsPath)
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the root without finding the file
			break
		}
		dir = parent
	}

	return nil, fmt.Errorf("Directory.Packages.props not found")
}

// parseCPMFile reads and parses a Directory.Packages.props file.
func parseCPMFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read Directory.Packages.props: %w", err)
	}

	var project cpmXML
	if err := xml.Unmarshal(data, &project); err != nil {
		return nil, fmt.Errorf("failed to parse Directory.Packages.props XML: %w", err)
	}

	// Build version map (case-insensitive keys since NuGet package names are case-insensitive)
	versions := make(map[string]string)
	for _, itemGroup := range project.ItemGroups {
		for _, pkgVer := range itemGroup.PackageVersions {
			if pkgVer.Include != "" && pkgVer.Version != "" {
				versions[strings.ToLower(pkgVer.Include)] = pkgVer.Version
			}
		}
	}

	return versions, nil
}

// XML structure for Directory.Packages.props
type cpmXML struct {
	XMLName    xml.Name       `xml:"Project"`
	ItemGroups []cpmItemGroup `xml:"ItemGroup"`
}

type cpmItemGroup struct {
	PackageVersions []cpmPackageVersion `xml:"PackageVersion"`
}

type cpmPackageVersion struct {
	Include string `xml:"Include,attr"`
	Version string `xml:"Version,attr"`
}

// resolve fetches transitive dependencies for all direct dependencies.
// It merges the sub-graphs from each package into a single graph.
func (p *CsProj) resolve(ctx context.Context, pkgs []string, opts deps.Options) (*dag.DAG, error) {
	return resolveTransitive(ctx, p.resolver, pkgs, opts)
}
