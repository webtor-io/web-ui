package common

import "github.com/webtor-io/web-ui/models"

// CheckAvailabilityResult represents the result of availability check
type CheckAvailabilityResult struct {
	ServiceType models.StreamingBackendType `json:"service_type"`
	Cached      bool                        `json:"cached"`
}

// LinkResult represents the result of link resolution
type LinkResult struct {
	URL         string                      `json:"url"`
	ServiceType models.StreamingBackendType `json:"service_type"`
	Cached      bool                        `json:"cached"`
}
