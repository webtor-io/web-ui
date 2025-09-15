package stremio

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAddonValidator_ValidateAddonURL(t *testing.T) {
	// Valid Torrentio manifest.json response
	validTorrentioManifest := `{
		"id": "com.stremio.torrentio.addon",
		"version": "0.0.15",
		"name": "Torrentio",
		"description": "Provides torrent streams from scraped torrent providers. Currently supports YTS(+), EZTV(+), RARBG(+), 1337x(+), ThePirateBay(+), KickassTorrents(+), TorrentGalaxy(+), MagnetDL(+), HorribleSubs(+), NyaaSi(+), TokyoTosho(+), AniDex(+), Rutor(+), Rutracker(+), Comando(+), BluDV(+), Torrent9(+), ilCorSaRoNeRo(+), MejorTorrent(+), Wolfmax4k(+), Cinecalidad(+), BestTorrents(+). To configure providers, RealDebrid/Premiumize/AllDebrid/DebridLink/EasyDebrid/Offcloud/TorBox/Put.io support and other settings visit https://torrentio.strem.fun",
		"catalogs": [],
		"resources": [
			{
				"name": "stream",
				"types": [
					"movie",
					"series",
					"anime"
				],
				"idPrefixes": [
					"tt",
					"kitsu"
				]
			}
		],
		"types": [
			"movie",
			"series",
			"anime",
			"other"
		],
		"background": "https://torrentio.strem.fun/images/background_v1.jpg",
		"logo": "https://torrentio.strem.fun/images/logo_v1.png",
		"behaviorHints": {
			"configurable": true,
			"configurationRequired": false
		}
	}`

	// Minimal valid manifest
	minimalValidManifest := `{
		"id": "com.example.addon",
		"version": "1.0.0",
		"name": "Test Addon",
		"description": "A test addon",
		"resources": ["stream"],
		"types": ["movie"]
	}`

	tests := []struct {
		name           string
		serverResponse string
		statusCode     int
		contentType    string
		wantErr        bool
		errContains    string
	}{
		{
			name:           "valid torrentio manifest",
			serverResponse: validTorrentioManifest,
			statusCode:     200,
			contentType:    "application/json",
			wantErr:        false,
		},
		{
			name:           "minimal valid manifest",
			serverResponse: minimalValidManifest,
			statusCode:     200,
			contentType:    "application/json",
			wantErr:        false,
		},
		{
			name:           "server returns 404",
			serverResponse: "Not Found",
			statusCode:     404,
			contentType:    "text/plain",
			wantErr:        true,
			errContains:    "addon URL returned HTTP 404",
		},
		{
			name:           "server returns 500",
			serverResponse: "Internal Server Error",
			statusCode:     500,
			contentType:    "text/plain",
			wantErr:        true,
			errContains:    "addon URL returned HTTP 500",
		},
		{
			name:           "invalid json response",
			serverResponse: "invalid json {",
			statusCode:     200,
			contentType:    "application/json",
			wantErr:        true,
			errContains:    "invalid JSON response from addon URL",
		},
		{
			name:           "missing required field - id",
			serverResponse: `{"version": "1.0.0", "name": "Test", "description": "Test", "resources": ["stream"], "types": ["movie"]}`,
			statusCode:     200,
			contentType:    "application/json",
			wantErr:        true,
			errContains:    "manifest missing required field: id",
		},
		{
			name:           "missing required field - version",
			serverResponse: `{"id": "test", "name": "Test", "description": "Test", "resources": ["stream"], "types": ["movie"]}`,
			statusCode:     200,
			contentType:    "application/json",
			wantErr:        true,
			errContains:    "manifest missing required field: version",
		},
		{
			name:           "missing required field - name",
			serverResponse: `{"id": "test", "version": "1.0.0", "description": "Test", "resources": ["stream"], "types": ["movie"]}`,
			statusCode:     200,
			contentType:    "application/json",
			wantErr:        true,
			errContains:    "manifest missing required field: name",
		},
		{
			name:           "missing required field - description",
			serverResponse: `{"id": "test", "version": "1.0.0", "name": "Test", "resources": ["stream"], "types": ["movie"]}`,
			statusCode:     200,
			contentType:    "application/json",
			wantErr:        true,
			errContains:    "manifest missing required field: description",
		},
		{
			name:           "missing required field - resources",
			serverResponse: `{"id": "test", "version": "1.0.0", "name": "Test", "description": "Test", "types": ["movie"]}`,
			statusCode:     200,
			contentType:    "application/json",
			wantErr:        true,
			errContains:    "manifest missing required field: resources",
		},
		{
			name:           "missing required field - types",
			serverResponse: `{"id": "test", "version": "1.0.0", "name": "Test", "description": "Test", "resources": ["stream"]}`,
			statusCode:     200,
			contentType:    "application/json",
			wantErr:        true,
			errContains:    "manifest missing required field: types",
		},
		{
			name:           "empty resources array",
			serverResponse: `{"id": "test", "version": "1.0.0", "name": "Test", "description": "Test", "resources": [], "types": ["movie"]}`,
			statusCode:     200,
			contentType:    "application/json",
			wantErr:        true,
			errContains:    "manifest missing required field: resources (must be non-empty array)",
		},
		{
			name:           "empty types array",
			serverResponse: `{"id": "test", "version": "1.0.0", "name": "Test", "description": "Test", "resources": ["stream"], "types": []}`,
			statusCode:     200,
			contentType:    "application/json",
			wantErr:        true,
			errContains:    "manifest missing required field: types (must be non-empty array)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.serverResponse))
			}))
			defer server.Close()

			// Create validator with test HTTP client
			client := &http.Client{Timeout: 5 * time.Second}
			validator := NewAddonValidator(client)

			// Test validation
			err := validator.ValidateAddonURL(server.URL + "/manifest.json")

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateAddonURL() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateAddonURL() error = %v, expected to contain %v", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateAddonURL() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestAddonValidator_ValidateAddonURL_Timeout(t *testing.T) {
	// Create a server that doesn't respond quickly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Sleep longer than client timeout
		w.WriteHeader(200)
		w.Write([]byte(`{"id": "test", "version": "1.0.0", "name": "Test", "description": "Test", "resources": ["stream"], "types": ["movie"]}`))
	}))
	defer server.Close()

	// Create validator with short timeout
	client := &http.Client{Timeout: 500 * time.Millisecond}
	validator := NewAddonValidator(client)

	err := validator.ValidateAddonURL(server.URL + "/manifest.json")

	if err == nil {
		t.Error("ValidateAddonURL() expected timeout error but got none")
		return
	}

	if !strings.Contains(err.Error(), "addon URL is not accessible") {
		t.Errorf("ValidateAddonURL() expected timeout error, got: %v", err)
	}
}

func TestAddonValidator_ValidateAddonURL_InvalidURL(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	validator := NewAddonValidator(client)

	tests := []struct {
		name string
		url  string
	}{
		{
			name: "invalid URL scheme",
			url:  "://invalid-url",
		},
		{
			name: "malformed URL",
			url:  "http://[invalid-host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateAddonURL(tt.url)
			if err == nil {
				t.Error("ValidateAddonURL() expected error for invalid URL but got none")
				return
			}

			if !strings.Contains(err.Error(), "failed to create request") && !strings.Contains(err.Error(), "addon URL is not accessible") {
				t.Errorf("ValidateAddonURL() expected fetch or create request error, got: %v", err)
			}
		})
	}
}

func TestAddonValidator_ValidateManifest_EdgeCases(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	validator := NewAddonValidator(client)

	tests := []struct {
		name        string
		manifest    string
		wantErr     bool
		errContains string
	}{
		{
			name: "manifest with extra fields",
			manifest: `{
				"id": "test.addon", 
				"version": "1.0.0", 
				"name": "Test Addon", 
				"description": "A test addon", 
				"resources": ["stream"], 
				"types": ["movie"],
				"extraField": "should be ignored",
				"anotherExtra": 123
			}`,
			wantErr: false,
		},
		{
			name: "manifest with complex resources",
			manifest: `{
				"id": "test.addon", 
				"version": "1.0.0", 
				"name": "Test Addon", 
				"description": "A test addon", 
				"resources": [
					{
						"name": "stream",
						"types": ["movie", "series"],
						"idPrefixes": ["tt"]
					},
					{
						"name": "meta",
						"types": ["movie"]
					}
				], 
				"types": ["movie", "series"]
			}`,
			wantErr: false,
		},
		{
			name: "empty string fields",
			manifest: `{
				"id": "", 
				"version": "1.0.0", 
				"name": "Test Addon", 
				"description": "A test addon", 
				"resources": ["stream"], 
				"types": ["movie"]
			}`,
			wantErr:     true,
			errContains: "manifest missing required field: id",
		},
		{
			name: "whitespace-only fields",
			manifest: `{
				"id": "   ", 
				"version": "1.0.0", 
				"name": "Test Addon", 
				"description": "A test addon", 
				"resources": ["stream"], 
				"types": ["movie"]
			}`,
			wantErr:     true,
			errContains: "manifest missing required field: id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(200)
				w.Write([]byte(tt.manifest))
			}))
			defer server.Close()

			err := validator.ValidateAddonURL(server.URL + "/manifest.json")

			if tt.wantErr {
				if err == nil {
					t.Error("ValidateAddonURL() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateAddonURL() error = %v, expected to contain %v", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateAddonURL() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestAddonValidator_RealWorldExample(t *testing.T) {
	// Test with actual Torrentio structure but with mock server
	// to ensure our validator works with real addon manifests
	torrentioManifest := `{
		"id": "com.stremio.torrentio.addon",
		"version": "0.0.15",
		"name": "Torrentio",
		"description": "Provides torrent streams from scraped torrent providers. Currently supports YTS(+), EZTV(+), RARBG(+), 1337x(+), ThePirateBay(+), KickassTorrents(+), TorrentGalaxy(+), MagnetDL(+), HorribleSubs(+), NyaaSi(+), TokyoTosho(+), AniDex(+), Rutor(+), Rutracker(+), Comando(+), BluDV(+), Torrent9(+), ilCorSaRoNeRo(+), MejorTorrent(+), Wolfmax4k(+), Cinecalidad(+), BestTorrents(+). To configure providers, RealDebrid/Premiumize/AllDebrid/DebridLink/EasyDebrid/Offcloud/TorBox/Put.io support and other settings visit https://torrentio.strem.fun",
		"catalogs": [],
		"resources": [
			{
				"name": "stream",
				"types": [
					"movie",
					"series",
					"anime"
				],
				"idPrefixes": [
					"tt",
					"kitsu"
				]
			}
		],
		"types": [
			"movie",
			"series",
			"anime",
			"other"
		],
		"background": "https://torrentio.strem.fun/images/background_v1.jpg",
		"logo": "https://torrentio.strem.fun/images/logo_v1.png",
		"behaviorHints": {
			"configurable": true,
			"configurationRequired": false
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(torrentioManifest))
		} else {
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	validator := NewAddonValidator(client)

	err := validator.ValidateAddonURL(server.URL + "/manifest.json")
	if err != nil {
		t.Errorf("ValidateAddonURL() failed with Torrentio example: %v", err)
	}
}

// Benchmark the validator with a typical manifest
func BenchmarkAddonValidator_ValidateAddonURL(b *testing.B) {
	manifest := `{
		"id": "com.example.addon",
		"version": "1.0.0",
		"name": "Example Addon",
		"description": "An example addon for benchmarking",
		"resources": ["stream", "meta"],
		"types": ["movie", "series"]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(manifest))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	validator := NewAddonValidator(client)

	url := server.URL + "/manifest.json"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := validator.ValidateAddonURL(url)
		if err != nil {
			b.Fatalf("Unexpected error: %v", err)
		}
	}
}
