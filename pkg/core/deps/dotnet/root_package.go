package dotnet

import "path/filepath"

func rootPackageFromPath(path string) string {
	dir := filepath.Dir(path)
	return filepath.Base(dir)
}
