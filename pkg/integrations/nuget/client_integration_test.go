//go:build integration

package nuget

import (
	"context"
	"testing"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"
)

// TestClient_FetchPackage_Integration tests fetching a real package from NuGet.org.
// Run with: go test -v -tags=integration ./pkg/integrations/nuget
func TestClient_FetchPackage_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), 1*time.Hour)

	// Test with a well-known package: Newtonsoft.Json
	pkg, err := client.FetchPackage(context.Background(), "Newtonsoft.Json", false)
	if err != nil {
		t.Fatalf("FetchPackage() error = %v", err)
	}

	// Verify basic fields
	if pkg.Name == "" {
		t.Error("FetchPackage() returned empty name")
	}
	if pkg.Version == "" {
		t.Error("FetchPackage() returned empty version")
	}

	t.Logf("Successfully fetched: %s v%s", pkg.Name, pkg.Version)
	t.Logf("Description: %s", pkg.Description)
	t.Logf("Dependencies: %v", pkg.Dependencies)
}
