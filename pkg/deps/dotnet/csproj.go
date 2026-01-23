package dotnet

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"

	"github.com/matzehuels/stacktower/pkg/dag"
	"github.com/matzehuels/stacktower/pkg/deps"
)

// CsProj parses .csproj files (modern SDK-style .NET Core/.NET 5+ format).
// Format: MSBuild XML file with PackageReference elements.
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
type CsProj struct {
	resolver deps.Resolver
}

// Type returns the manifest type identifier.
func (p *CsProj) Type() string {
	return "csproj"
}

// IncludesTransitive returns false because .csproj only lists direct dependencies.
func (p *CsProj) IncludesTransitive() bool {
	return false
}

// Supports checks if the filename matches this parser.
func (p *CsProj) Supports(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".csproj")
}

// Parse reads a .csproj file and builds a dependency graph.
// It only includes direct dependencies; transitive dependencies must be resolved via the registry.
func (p *CsProj) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
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

	// Create graph
	g := dag.New(nil)

	// Add root node (project) - use placeholder
	rootID := "__project__"
	g.AddNode(dag.Node{ID: rootID, Row: 0})

	// Try to infer project name from filename
	projectName := strings.TrimSuffix(path, ".csproj")
	if idx := strings.LastIndexAny(projectName, "/\\"); idx >= 0 {
		projectName = projectName[idx+1:]
	}
	if projectName == "" {
		projectName = ""
	}

	// Add direct dependencies as edges
	for _, itemGroup := range project.ItemGroups {
		for _, pkg := range itemGroup.PackageReferences {
			if pkg.Include != "" {
				g.AddNode(dag.Node{ID: pkg.Include})
				g.AddEdge(dag.Edge{From: rootID, To: pkg.Include})
			}
		}
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
}

type csprojPackageReference struct {
	Include string `xml:"Include,attr"`
	Version string `xml:"Version,attr"`
}
