package stremio

import (
	"context"
	"strings"
	"testing"
)

func TestNewManifest(t *testing.T) {
	domain := "https://test.example.com"

	manifest := NewManifest(domain, nil, false, 0)

	if manifest == nil {
		t.Fatal("NewManifest returned nil")
	}

	if manifest.domain != domain {
		t.Errorf("Expected domain '%s', got '%s'", domain, manifest.domain)
	}
}

func TestManifest_GetManifest(t *testing.T) {
	domain := "https://webtor.io"
	manifest := NewManifest(domain, nil, false, 0)

	ctx := context.Background()
	response, err := manifest.GetManifest(ctx)

	if err != nil {
		t.Fatalf("GetManifest failed: %v", err)
	}

	if response == nil {
		t.Fatal("GetManifest returned nil response")
	}

	// Test manifest fields
	if response.Id != "org.stremio.webtor.io" {
		t.Errorf("Expected ID 'org.stremio.webtor.io', got '%s'", response.Id)
	}

	if response.Version != "0.1.0" {
		t.Errorf("Expected Version '0.1.0', got '%s'", response.Version)
	}

	if response.Name != "Webtor.io" {
		t.Errorf("Expected Name 'Webtor.io', got '%s'", response.Name)
	}

	if response.Description != manifestDescription {
		t.Errorf("Description = %q, want %q", response.Description, manifestDescription)
	}

	// Test types
	expectedTypes := []string{"movie", "series"}
	if len(response.Types) != len(expectedTypes) {
		t.Errorf("Expected %d types, got %d", len(expectedTypes), len(response.Types))
	}
	for i, expectedType := range expectedTypes {
		if i >= len(response.Types) || response.Types[i] != expectedType {
			t.Errorf("Expected type[%d] '%s', got '%s'", i, expectedType, response.Types[i])
		}
	}

	// Test catalogs
	expectedCatalogs := []CatalogItem{
		{"movie", catalogID},
		{"series", catalogID},
	}
	if len(response.Catalogs) != len(expectedCatalogs) {
		t.Errorf("Expected %d catalogs, got %d", len(expectedCatalogs), len(response.Catalogs))
	}
	for i, expectedCatalog := range expectedCatalogs {
		if i >= len(response.Catalogs) {
			t.Errorf("Missing catalog at index %d", i)
			continue
		}
		if response.Catalogs[i].Type != expectedCatalog.Type {
			t.Errorf("Expected catalog[%d].Type '%s', got '%s'", i, expectedCatalog.Type, response.Catalogs[i].Type)
		}
		if response.Catalogs[i].Id != expectedCatalog.Id {
			t.Errorf("Expected catalog[%d].Id '%s', got '%s'", i, expectedCatalog.Id, response.Catalogs[i].Id)
		}
	}

	// Test resources (Resources is interface{} but should contain []string)
	expectedResources := []string{"stream", "catalog", "meta"}
	resourcesSlice, ok := response.Resources.([]string)
	if !ok {
		t.Errorf("Expected Resources to be []string, got %T", response.Resources)
	} else {
		if len(resourcesSlice) != len(expectedResources) {
			t.Errorf("Expected %d resources, got %d", len(expectedResources), len(resourcesSlice))
		}
		for i, expectedResource := range expectedResources {
			if i >= len(resourcesSlice) || resourcesSlice[i] != expectedResource {
				t.Errorf("Expected resource[%d] '%s', got '%s'", i, expectedResource, resourcesSlice[i])
			}
		}
	}

	// Test logo URL contains domain
	expectedLogoSuffix := "/assets/night/android-chrome-256x256.png"
	expectedLogo := domain + expectedLogoSuffix
	if response.Logo != expectedLogo {
		t.Errorf("Expected Logo '%s', got '%s'", expectedLogo, response.Logo)
	}

	// Test contact email
	if response.ContactEmail != "support@webtor.io" {
		t.Errorf("Expected ContactEmail 'support@webtor.io', got '%s'", response.ContactEmail)
	}
}

func TestManifest_GetManifest_WithDifferentDomain(t *testing.T) {
	domain := "https://custom.domain.com"
	manifest := NewManifest(domain, nil, false, 0)

	ctx := context.Background()
	response, err := manifest.GetManifest(ctx)

	if err != nil {
		t.Fatalf("GetManifest failed: %v", err)
	}

	// Verify logo uses the custom domain
	expectedLogo := domain + "/assets/night/android-chrome-256x256.png"
	if response.Logo != expectedLogo {
		t.Errorf("Expected Logo '%s', got '%s'", expectedLogo, response.Logo)
	}

	// Other fields should remain the same
	if response.Id != "org.stremio.webtor.io" {
		t.Errorf("ID should not change with different domain")
	}
	if response.Name != "Webtor.io" {
		t.Errorf("Name should not change with different domain")
	}
}

func TestManifest_ImplementsInterface(t *testing.T) {
	manifest := NewManifest("https://test.com", nil, false, 0)

	// Verify it implements ManifestService interface
	_, ok := interface{}(manifest).(ManifestService)
	if !ok {
		t.Error("Manifest should implement ManifestService interface")
	}
}

func TestManifest_CatalogIDConstant(t *testing.T) {
	// Test that the catalogID constant is used correctly
	domain := "https://test.com"
	manifest := NewManifest(domain, nil, false, 0)

	ctx := context.Background()
	response, err := manifest.GetManifest(ctx)

	if err != nil {
		t.Fatalf("GetManifest failed: %v", err)
	}

	// Verify all catalogs use the catalogID constant
	for i, catalog := range response.Catalogs {
		if catalog.Id != catalogID {
			t.Errorf("Catalog[%d] should use catalogID constant '%s', got '%s'", i, catalogID, catalog.Id)
		}
	}
}

// TestManifestDescriptionQuotesTheFreeCapOnlyFromTheCatalog: the speed a
// Stremio user will get is the plan's, and the plan is data. A deployment
// without a catalog (nothing sold, nothing capped) quotes no speed at all.
func TestManifestDescriptionQuotesTheFreeCapOnlyFromTheCatalog(t *testing.T) {
	withCap, err := NewManifest("https://webtor.io", nil, false, 5).GetManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := "up to 5\u00a0Mbit/s."; !strings.HasSuffix(withCap.Description, want) {
		t.Errorf("with a 5 Mbit/s free plan the description should end with %q, got %q", want, withCap.Description)
	}
	if !strings.HasPrefix(withCap.Description, manifestDescription) {
		t.Errorf("the cap sentence should follow the base description, got %q", withCap.Description)
	}
	noCap, err := NewManifest("https://webtor.io", nil, false, 0).GetManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(noCap.Description, "Mbit/s") {
		t.Errorf("without a catalog the description must not quote a speed, got %q", noCap.Description)
	}
}

// TestManifestDescriptionKeepsToTheFacts: a stream starts once the swarm
// has delivered enough of the file, which takes from seconds to minutes, so
// the addon card in Stremio must not promise an instant start.
func TestManifestDescriptionKeepsToTheFacts(t *testing.T) {
	for _, mbps := range []int64{0, 5} {
		d := strings.ToLower(NewManifest("https://webtor.io", nil, false, mbps).Description())
		for _, banned := range []string{"instant", "no downloading", "just click and play"} {
			if strings.Contains(d, banned) {
				t.Errorf("description (free cap %d) promises %q: %q", mbps, banned, d)
			}
		}
	}
}
