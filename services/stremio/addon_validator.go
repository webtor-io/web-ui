package stremio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

type AddonValidator struct {
	client    *http.Client
	userAgent string
}

func NewAddonValidator(c *cli.Context, client *http.Client) *AddonValidator {
	return &AddonValidator{
		client:    client,
		userAgent: c.String(StremioUserAgentFlag),
	}
}

// ValidateURL checks if the addon URL is available and has valid manifest structure
func (av *AddonValidator) ValidateURL(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return errors.Wrap(err, "failed to create request")
	}

	// Set appropriate headers
	req.Header.Set("Accept", "application/json")
	if av.userAgent != "" {
		req.Header.Set("User-Agent", av.userAgent)
	}

	// Make HTTP request
	resp, err := av.client.Do(req)
	if err != nil {
		return errors.Wrap(err, "addon URL is not accessible")
	}
	defer resp.Body.Close()

	// Check HTTP status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("addon URL returned HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Check Content-Type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" && contentType != "application/json" && contentType != "application/json; charset=utf-8" {
		return fmt.Errorf("addon URL must return JSON content, got: %s", contentType)
	}

	// Parse JSON response
	var manifest ManifestResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&manifest); err != nil {
		return errors.Wrap(err, "invalid JSON response from addon URL")
	}

	// Validate required manifest fields
	if err := av.validateManifest(&manifest); err != nil {
		return errors.Wrap(err, "invalid Stremio addon manifest")
	}

	return nil
}

// validateManifest validates the structure and required fields of a Stremio addon manifest
func (av *AddonValidator) validateManifest(manifest *ManifestResponse) error {
	if strings.TrimSpace(manifest.Id) == "" {
		return errors.New("manifest missing required field: id")
	}

	if strings.TrimSpace(manifest.Version) == "" {
		return errors.New("manifest missing required field: version")
	}

	if strings.TrimSpace(manifest.Name) == "" {
		return errors.New("manifest missing required field: name")
	}

	if strings.TrimSpace(manifest.Description) == "" {
		return errors.New("manifest missing required field: description")
	}

	if len(manifest.Types) == 0 {
		return errors.New("manifest missing required field: types (must be non-empty array)")
	}

	// Validate resources field (can be []string or []map[string]interface{})
	if manifest.Resources == nil {
		return errors.New("manifest missing required field: resources (must be non-empty array)")
	}

	hasResources := false
	switch resources := manifest.Resources.(type) {
	case []interface{}:
		if len(resources) == 0 {
			return errors.New("manifest missing required field: resources (must be non-empty array)")
		}
		hasResources = true

		// Validate that resources contains valid Stremio resources
		validResources := map[string]bool{
			"catalog":   true,
			"meta":      true,
			"stream":    true,
			"subtitles": true,
		}

		hasValidResource := false
		for _, res := range resources {
			var resourceName string

			// Handle both string resources and object resources
			switch r := res.(type) {
			case string:
				resourceName = r
			case map[string]interface{}:
				if name, ok := r["name"].(string); ok {
					resourceName = name
				}
			}

			if validResources[resourceName] {
				hasValidResource = true
				break
			}
		}

		if !hasValidResource {
			return errors.New("manifest resources must include at least one valid Stremio resource (catalog, meta, stream, subtitles)")
		}

	case []string:
		if len(resources) == 0 {
			return errors.New("manifest missing required field: resources (must be non-empty array)")
		}
		hasResources = true

		// Validate that resources contains valid Stremio resources
		validResources := map[string]bool{
			"catalog":   true,
			"meta":      true,
			"stream":    true,
			"subtitles": true,
		}

		hasValidResource := false
		for _, resource := range resources {
			if validResources[resource] {
				hasValidResource = true
				break
			}
		}

		if !hasValidResource {
			return errors.New("manifest resources must include at least one valid Stremio resource (catalog, meta, stream, subtitles)")
		}
	default:
		return errors.New("manifest missing required field: resources (must be non-empty array)")
	}

	if !hasResources {
		return errors.New("manifest missing required field: resources (must be non-empty array)")
	}

	// Validate that types contains valid Stremio types
	validTypes := map[string]bool{
		"movie":   true,
		"series":  true,
		"channel": true,
		"tv":      true,
		"anime":   true,
		"other":   true,
	}

	hasValidType := false
	for _, addonType := range manifest.Types {
		if validTypes[addonType] {
			hasValidType = true
			break
		}
	}

	if !hasValidType {
		return errors.New("manifest types must include at least one valid Stremio type (movie, series, channel, tv, anime, other)")
	}

	return nil
}
