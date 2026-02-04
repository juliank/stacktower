package dotnet

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

func parseCPMProps(path string) ([]cpmPackageVersion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read Directory.Packages.props: %w", err)
	}

	var project cpmXML
	if err := xml.Unmarshal(data, &project); err != nil {
		return nil, fmt.Errorf("failed to parse Directory.Packages.props XML: %w", err)
	}

	var versions []cpmPackageVersion
	for _, itemGroup := range project.ItemGroups {
		for _, pkgVer := range itemGroup.PackageVersions {
			if pkgVer.Include != "" && pkgVer.Version != "" {
				versions = append(versions, pkgVer)
			}
		}
	}

	return versions, nil
}

// parseCPMFile reads and parses a Directory.Packages.props file.
func parseCPMFile(path string) (map[string]string, error) {
	entries, err := parseCPMProps(path)
	if err != nil {
		return nil, err
	}

	// Build version map (case-insensitive keys since NuGet package names are case-insensitive)
	versions := make(map[string]string)
	for _, pkgVer := range entries {
		versions[strings.ToLower(pkgVer.Include)] = pkgVer.Version
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
