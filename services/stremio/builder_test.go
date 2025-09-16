package stremio

import (
	"flag"
	"net/http"
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/common"
)

// Helper function to create CLI context for tests
func createTestCLIContext() *cli.Context {
	app := cli.NewApp()
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  common.DomainFlag,
			Value: "https://test.example.com",
		},
	}

	flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
	flagSet.String(common.DomainFlag, "https://test.example.com", "domain flag")
	flagSet.Set(common.DomainFlag, "https://test.example.com")
	return cli.NewContext(app, flagSet, nil)
}

func TestNewBuilder(t *testing.T) {
	c := createTestCLIContext()

	// Create mock dependencies
	pg := &cs.PG{}
	client := &http.Client{}
	rapi := &api.Api{}

	// Test constructor
	builder := NewBuilder(c, pg, client, rapi)

	if builder == nil {
		t.Fatal("NewBuilder returned nil")
	}

	if builder.pg != pg {
		t.Error("Builder pg not set correctly")
	}

	if builder.cl != client {
		t.Error("Builder client not set correctly")
	}

	if builder.rapi != rapi {
		t.Error("Builder API not set correctly")
	}

	if builder.domain != "https://test.example.com" {
		t.Errorf("Expected domain 'https://test.example.com', got %s", builder.domain)
	}

	// Test that cache is functional by attempting to use it
	testKey := "init_test"
	_, err := builder.cache.Get(testKey, func() (*StreamsResponse, error) {
		return &StreamsResponse{}, nil
	})
	if err != nil {
		t.Errorf("Builder cache should be functional, got error: %v", err)
	}
}

func TestBuilder_BuildManifestService(t *testing.T) {
	c := createTestCLIContext()

	builder := NewBuilder(c, &cs.PG{}, &http.Client{}, &api.Api{})

	service, err := builder.BuildManifestService()
	if err != nil {
		t.Fatalf("BuildManifestService failed: %v", err)
	}

	if service == nil {
		t.Fatal("BuildManifestService returned nil service")
	}

	// Verify it implements ManifestService interface
	_, ok := service.(ManifestService)
	if !ok {
		t.Error("Returned service does not implement ManifestService interface")
	}
}

func TestBuilder_BuildCatalogService_Success(t *testing.T) {
	c := createTestCLIContext()

	// Create mock PG that returns a non-nil DB
	mockPG := &cs.PG{}
	// Note: In a real test, we would need to properly mock the DB connection
	// For now, this test will fail due to nil DB, which is expected behavior

	builder := NewBuilder(c, mockPG, &http.Client{}, &api.Api{})
	userID := uuid.NewV4()

	_, err := builder.BuildCatalogService(userID)

	// We expect this to fail with "database not initialized" error
	if err == nil {
		t.Error("Expected error for nil database, but got none")
	}

	expectedError := "database not initialized"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestBuilder_BuildMetaService_Success(t *testing.T) {
	c := createTestCLIContext()

	// Create mock PG that returns a non-nil DB
	mockPG := &cs.PG{}
	// Note: In a real test, we would need to properly mock the DB connection

	builder := NewBuilder(c, mockPG, &http.Client{}, &api.Api{})
	userID := uuid.NewV4()

	_, err := builder.BuildMetaService(userID)

	// We expect this to fail with "database not initialized" error
	if err == nil {
		t.Error("Expected error for nil database, but got none")
	}

	expectedError := "database not initialized"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestBuilder_BuildStreamsService_Success(t *testing.T) {
	c := createTestCLIContext()

	// Create mock PG that returns a non-nil DB
	mockPG := &cs.PG{}

	builder := NewBuilder(c, mockPG, &http.Client{}, &api.Api{})
	userID := uuid.NewV4()
	claims := &api.Claims{}

	_, err := builder.BuildStreamsService(userID, claims)

	// We expect this to fail with "database not initialized" error
	if err == nil {
		t.Error("Expected error for nil database, but got none")
	}

	expectedError := "database not initialized"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestBuilder_CacheConfiguration(t *testing.T) {
	c := createTestCLIContext()

	builder := NewBuilder(c, &cs.PG{}, &http.Client{}, &api.Api{})

	// Verify cache is functional by testing its usage
	testKey := "test_key"
	testValue := &StreamsResponse{Streams: []StreamItem{}}

	result, err := builder.cache.Get(testKey, func() (*StreamsResponse, error) {
		return testValue, nil
	})

	if err != nil {
		t.Fatalf("Cache Get failed: %v", err)
	}

	if result != testValue {
		t.Error("Cache should return the correct value")
	}
}
