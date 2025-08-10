package webdav

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
)

// buildAllContentResponse builds the response for the /all/ directory
func (s *Handler) buildAllContentResponse(c *gin.Context, requestPath string, depth string) ([]Response, error) {
	var responses []Response
	
	// Add the all directory itself
	response := s.createDirectoryResponse("/"+requestPath, "All Content")
	responses = append(responses, response)
	
	if depth == "0" {
		return responses, nil
	}

	// Get user's complete library - ALL torrents, not just movies/series
	ctx := c.Request.Context()
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	// Get ALL torrents from library
	libraryEntries, err := models.GetLibraryTorrentsList(ctx, db, u.ID, models.SortTypeRecentlyAdded)
	if err != nil {
		return nil, err
	}

	// Add content for each torrent
	for _, entry := range libraryEntries {
		if entry.Torrent == nil {
			continue // Skip entries without torrent data
		}
		
		torrentResponses, err := s.buildTorrentContentResponses(ctx, requestPath, entry)
		if err != nil {
			continue // Skip on error
		}
		responses = append(responses, torrentResponses...)
	}

	return responses, nil
}

// buildMoviesResponse builds the response for the /movies/ directory
func (s *Handler) buildMoviesResponse(c *gin.Context, requestPath string, depth string) ([]Response, error) {
	var responses []Response
	
	// Add the movies directory itself
	response := s.createDirectoryResponse("/"+requestPath, "Movies")
	responses = append(responses, response)
	
	if depth == "0" {
		return responses, nil
	}

	// Get user's complete library and filter for movies
	ctx := c.Request.Context()
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	libraryEntries, err := models.GetLibraryTorrentsList(ctx, db, u.ID, models.SortTypeRecentlyAdded)
	if err != nil {
		return nil, err
	}

	// Add content for movie torrents only
	for _, entry := range libraryEntries {
		if entry.Torrent == nil || entry.MediaInfo == nil {
			continue
		}
		
		// Filter for movies only
		if entry.MediaInfo.MediaType == nil || *entry.MediaInfo.MediaType != 1 { // Assuming 1 = movie
			continue
		}
		
		torrentResponses, err := s.buildTorrentContentResponses(ctx, requestPath, entry)
		if err != nil {
			continue // Skip on error
		}
		responses = append(responses, torrentResponses...)
	}

	return responses, nil
}

// buildSeriesResponse builds the response for the /series/ directory
func (s *Handler) buildSeriesResponse(c *gin.Context, requestPath string, depth string) ([]Response, error) {
	var responses []Response
	
	// Add the series directory itself
	response := s.createDirectoryResponse("/"+requestPath, "Series")
	responses = append(responses, response)
	
	if depth == "0" {
		return responses, nil
	}

	// Get user's complete library and filter for series
	ctx := c.Request.Context()
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	libraryEntries, err := models.GetLibraryTorrentsList(ctx, db, u.ID, models.SortTypeRecentlyAdded)
	if err != nil {
		return nil, err
	}

	// Add content for series torrents only
	for _, entry := range libraryEntries {
		if entry.Torrent == nil || entry.MediaInfo == nil {
			continue
		}
		
		// Filter for series only
		if entry.MediaInfo.MediaType == nil || *entry.MediaInfo.MediaType != 2 { // Assuming 2 = series
			continue
		}
		
		torrentResponses, err := s.buildTorrentContentResponses(ctx, requestPath, entry)
		if err != nil {
			continue // Skip on error
		}
		responses = append(responses, torrentResponses...)
	}

	return responses, nil
}

// buildTorrentContentResponses builds WebDAV responses for a torrent's content using generic approach
func (s *Handler) buildTorrentContentResponses(ctx context.Context, requestPath string, entry *models.Library) ([]Response, error) {
	var responses []Response
	
	if entry.Torrent == nil {
		return responses, nil
	}
	
	// Create a directory for the torrent
	torrentName := sanitizeFilename(entry.Torrent.Name)
	torrentDir := fmt.Sprintf("/%s/%s/", requestPath, torrentName)
	response := s.createDirectoryResponse(torrentDir, torrentName)
	responses = append(responses, response)

	// Get torrent files using the API
	torrentItems, err := s.retrieveTorrentItems(ctx, entry.ResourceID)
	if err != nil {
		return responses, err // Return directory only if we can't get files
	}

	// Add each file in the torrent
	for _, item := range torrentItems {
		if item.Type != "file" {
			continue // Skip directories
		}
		
		filename := filepath.Base(item.PathStr)
		if filename == "" || filename == "." {
			filename = sanitizeFilename(item.Name)
		}
		
		href := fmt.Sprintf("/%s/%s/%s", requestPath, torrentName, filename)
		size := item.Size
		modTime := time.Now().Format(time.RFC1123)
		contentType := detectContentType(filename)
		
		response := s.createFileResponse(href, filename, size, contentType, modTime)
		responses = append(responses, response)
	}

	return responses, nil
}

// handleContentFileDownload handles downloading content files using the Webtor streaming API
func (s *Handler) handleContentFileDownload(c *gin.Context, folderType FolderType, filePath string) {
	// Parse the file path to extract content info
	parts := strings.Split(filePath, "/")
	if len(parts) < 2 {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	torrentName := parts[0]
	filename := parts[1]

	// Find the torrent in the user's library using generic approach
	ctx := c.Request.Context()
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("database not initialized"))
		return
	}

	resourceID, itemPath, err := s.findTorrentFile(ctx, db, u.ID, torrentName, filename, folderType)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	if resourceID == "" || itemPath == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	// Get streaming URL using the Webtor API
	streamURL, err := s.getStreamingURL(ctx, c, resourceID, itemPath)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Redirect to the streaming URL
	c.Redirect(http.StatusFound, streamURL)
}

// findTorrentFile finds a file in the user's torrent library using generic approach
func (s *Handler) findTorrentFile(ctx context.Context, db interface{}, userID uuid.UUID, torrentName, filename string, folderType FolderType) (string, string, error) {
	pgDB, ok := db.(*pg.DB)
	if !ok {
		return "", "", errors.New("invalid database type")
	}
	
	// Get all torrents from library
	libraryEntries, err := models.GetLibraryTorrentsList(ctx, pgDB, userID, models.SortTypeRecentlyAdded)
	if err != nil {
		return "", "", err
	}

	// Find the matching torrent
	for _, entry := range libraryEntries {
		if entry.Torrent == nil {
			continue
		}
		
		// Check if torrent name matches
		if sanitizeFilename(entry.Torrent.Name) != torrentName {
			continue
		}
		
		// Apply folder type filtering if needed
		if folderType == FolderTypeMovies && entry.MediaInfo != nil && (entry.MediaInfo.MediaType == nil || *entry.MediaInfo.MediaType != 1) {
			continue
		}
		if folderType == FolderTypeSeries && entry.MediaInfo != nil && (entry.MediaInfo.MediaType == nil || *entry.MediaInfo.MediaType != 2) {
			continue
		}
		
		// Get torrent items to find the specific file
		torrentItems, err := s.retrieveTorrentItems(ctx, entry.ResourceID)
		if err != nil {
			continue // Skip on error
		}
		
		// Find the matching file
		for _, item := range torrentItems {
			if item.Type != "file" {
				continue
			}
			
			itemFilename := filepath.Base(item.PathStr)
			if itemFilename == filename || sanitizeFilename(item.Name) == filename {
				return entry.ResourceID, item.PathStr, nil
			}
		}
	}

	return "", "", nil
}

// retrieveTorrentItems retrieves all items from a torrent
func (s *Handler) retrieveTorrentItems(ctx context.Context, hash string) ([]ra.ListItem, error) {
	// Get API claims from context - we need this for API calls
	// For now, we'll create a basic claims object
	// TODO: This should be properly integrated with the authentication system
	claims := &api.Claims{
		SessionID: "", // This will need to be set properly
	}
	
	limit := uint(100)
	offset := uint(0)
	var items []ra.ListItem
	for {
		resp, err := s.sapi.ListResourceContent(ctx, claims, hash, &api.ListResourceContentArgs{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range resp.Items {
			items = append(items, item)
		}
		if (resp.Count - int(offset)) == len(resp.Items) {
			break
		}
		offset += limit
	}
	return items, nil
}

// getStreamingURL gets a streaming URL using the Webtor API
func (s *Handler) getStreamingURL(ctx context.Context, c *gin.Context, resourceID, itemPath string) (string, error) {
	// Get API claims from context
	cla := api.GetClaimsFromContext(c)
	if cla == nil {
		return "", errors.New("no API claims available")
	}

	// Find the torrent item
	ti, err := s.retrieveTorrentItem(ctx, resourceID, cla, itemPath)
	if err != nil {
		return "", err
	}

	if ti == nil {
		return "", errors.New("torrent item not found")
	}

	// Export the resource content
	er, err := s.sapi.ExportResourceContent(ctx, cla, resourceID, ti.ID, "")
	if err != nil {
		return "", err
	}

	downloadItem, exists := er.ExportItems["download"]
	if er.ExportItems == nil || !exists {
		return "", errors.New("download URL not available")
	}

	return downloadItem.URL, nil
}

// Helper functions

func sanitizeFilename(filename string) string {
	// Remove or replace characters that are problematic in filenames
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(filename)
}

func getFileExtension(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return ".mkv" // Default extension
	}
	return ext
}

// detectContentType detects MIME type using Go's standard library
func detectContentType(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return "application/octet-stream"
	}
	
	// Use Go's standard MIME type detection
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		return "application/octet-stream"
	}
	
	return mimeType
}
