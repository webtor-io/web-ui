package resource

import (
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/models"
)

// addResourceToSession adds a resource ID to the current session for guest access tracking
func (s *Handler) addResourceToSession(c *gin.Context, resourceID string) {
	session := sessions.Default(c)

	// Get existing resources from session
	sessionResources := session.Get("resources")
	var resources []string

	if sessionResources != nil {
		if existingResources, ok := sessionResources.([]string); ok {
			resources = existingResources
		}
	}

	// Check if resource is already in session
	for _, existing := range resources {
		if existing == resourceID {
			return // Already tracked
		}
	}

	// Add new resource to session (limit to 50 resources to prevent session bloat)
	resources = append(resources, resourceID)
	if len(resources) > 50 {
		resources = resources[len(resources)-50:] // Keep only last 50
	}

	session.Set("resources", resources)
	_ = session.Save()
}

// hasAccessPermission checks if user has permission to access the resource
func (s *Handler) hasAccessPermission(c *gin.Context, args *GetArgs) bool {
	// For guests, check if resource was added in current session
	session := sessions.Default(c)
	sessionResources := session.Get("resources")
	if sessionResources == nil {
		return false
	}

	resources, ok := sessionResources.([]string)
	if !ok {
		return false
	}

	// Check if resource ID is in session
	for _, resourceID := range resources {
		if resourceID == args.ID {
			return true
		}
	}

	// For authenticated users, check if resource is in their library
	if args.User.HasAuth() {
		db := s.pg.Get()
		if db == nil {
			return false
		}
		inLibrary, err := models.IsInLibrary(c.Request.Context(), db, args.User.ID, args.ID)
		if err != nil {
			return false
		}
		return inLibrary
	}

	return false
}
