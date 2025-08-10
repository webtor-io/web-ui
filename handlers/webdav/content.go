package webdav

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
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

	// Get user's complete library
	ctx := c.Request.Context()
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	// Get all movies and series
	movies, err := models.GetLibraryMovieList(ctx, db, u.ID, models.SortTypeRecentlyAdded)
	if err != nil {
		return nil, err
	}

	series, err := models.GetLibrarySeriesList(ctx, db, u.ID, models.SortTypeRecentlyAdded)
	if err != nil {
		return nil, err
	}

	// Add movie content
	for _, movie := range movies {
		movieResponses, err := s.buildMovieContentResponses(requestPath, movie)
		if err != nil {
			continue // Skip on error
		}
		responses = append(responses, movieResponses...)
	}

	// Add series content
	for _, serie := range series {
		seriesResponses, err := s.buildSeriesContentResponses(requestPath, serie)
		if err != nil {
			continue // Skip on error
		}
		responses = append(responses, seriesResponses...)
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

	// Get user's movie library
	ctx := c.Request.Context()
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	movies, err := models.GetLibraryMovieList(ctx, db, u.ID, models.SortTypeRecentlyAdded)
	if err != nil {
		return nil, err
	}

	// Add movie content
	for _, movie := range movies {
		movieResponses, err := s.buildMovieContentResponses(requestPath, movie)
		if err != nil {
			continue // Skip on error
		}
		responses = append(responses, movieResponses...)
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

	// Get user's series library
	ctx := c.Request.Context()
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	series, err := models.GetLibrarySeriesList(ctx, db, u.ID, models.SortTypeRecentlyAdded)
	if err != nil {
		return nil, err
	}

	// Add series content
	for _, serie := range series {
		seriesResponses, err := s.buildSeriesContentResponses(requestPath, serie)
		if err != nil {
			continue // Skip on error
		}
		responses = append(responses, seriesResponses...)
	}

	return responses, nil
}

// buildMovieContentResponses builds WebDAV responses for a movie's content
func (s *Handler) buildMovieContentResponses(requestPath string, movie models.VideoContentWithMetadata) ([]Response, error) {
	var responses []Response
	
	// Create a directory for the movie
	movieTitle := sanitizeFilename(movie.GetContent().Title)
	movieDir := fmt.Sprintf("/%s/%s/", requestPath, movieTitle)
	response := s.createDirectoryResponse(movieDir, movieTitle)
	responses = append(responses, response)

	// Add the movie file
	if movie.GetPath() != nil {
		path := *movie.GetPath()
		filename := filepath.Base(path)
		if filename == "" || filename == "." {
			filename = movieTitle + getFileExtension(path)
		}
		
		href := fmt.Sprintf("/%s/%s/%s", requestPath, movieTitle, filename)
		size := int64(0) // We don't have size info readily available
		modTime := time.Now().Format(time.RFC1123)
		contentType := getContentType(filename)
		
		response := s.createFileResponse(href, filename, size, contentType, modTime)
		responses = append(responses, response)
	}

	return responses, nil
}

// buildSeriesContentResponses builds WebDAV responses for a series' content
func (s *Handler) buildSeriesContentResponses(requestPath string, serie models.VideoContentWithMetadata) ([]Response, error) {
	var responses []Response
	
	// Create a directory for the series
	seriesTitle := sanitizeFilename(serie.GetContent().Title)
	seriesDir := fmt.Sprintf("/%s/%s/", requestPath, seriesTitle)
	response := s.createDirectoryResponse(seriesDir, seriesTitle)
	responses = append(responses, response)

	// Add episodes
	if series, ok := serie.(*models.Series); ok {
		for _, episode := range series.Episodes {
			if episode.Path == nil {
				continue
			}
			
			path := *episode.Path
			filename := filepath.Base(path)
			if filename == "" || filename == "." {
				// Generate filename from episode info
				if episode.Season != nil && episode.Episode != nil {
					filename = fmt.Sprintf("S%02dE%02d%s", *episode.Season, *episode.Episode, getFileExtension(path))
				} else {
					filename = seriesTitle + getFileExtension(path)
				}
			}
			
			href := fmt.Sprintf("/%s/%s/%s", requestPath, seriesTitle, filename)
			size := int64(0) // We don't have size info readily available
			modTime := time.Now().Format(time.RFC1123)
			contentType := getContentType(filename)
			
			response := s.createFileResponse(href, filename, size, contentType, modTime)
			responses = append(responses, response)
		}
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

	contentTitle := parts[0]
	filename := parts[1]

	// Find the content in the user's library
	ctx := c.Request.Context()
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("database not initialized"))
		return
	}

	var resourceID string
	var itemPath string
	var err error

	switch folderType {
	case FolderTypeMovies, FolderTypeAll:
		resourceID, itemPath, err = s.findMovieFile(ctx, db, u.ID.String(), contentTitle, filename)
	case FolderTypeSeries:
		resourceID, itemPath, err = s.findSeriesFile(ctx, db, u.ID.String(), contentTitle, filename)
	default:
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

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

// findMovieFile finds a movie file in the user's library
func (s *Handler) findMovieFile(ctx context.Context, db interface{}, userID string, contentTitle, filename string) (string, string, error) {
	pgDB, ok := db.(*pg.DB)
	if !ok {
		return "", "", errors.New("invalid database type")
	}
	
	userUUID, err := uuid.FromString(userID)
	if err != nil {
		return "", "", err
	}
	
	movies, err := models.GetLibraryMovieList(ctx, pgDB, userUUID, models.SortTypeRecentlyAdded)
	if err != nil {
		return "", "", err
	}

	for _, movie := range movies {
		if sanitizeFilename(movie.GetContent().Title) == contentTitle {
			if movie.GetPath() != nil {
				path := *movie.GetPath()
				if filepath.Base(path) == filename || 
				   sanitizeFilename(movie.GetContent().Title)+getFileExtension(path) == filename {
					return movie.GetContent().ResourceID, path, nil
				}
			}
		}
	}

	return "", "", nil
}

// findSeriesFile finds a series episode file in the user's library
func (s *Handler) findSeriesFile(ctx context.Context, db interface{}, userID string, contentTitle, filename string) (string, string, error) {
	pgDB, ok := db.(*pg.DB)
	if !ok {
		return "", "", errors.New("invalid database type")
	}
	
	userUUID, err := uuid.FromString(userID)
	if err != nil {
		return "", "", err
	}
	
	series, err := models.GetLibrarySeriesList(ctx, pgDB, userUUID, models.SortTypeRecentlyAdded)
	if err != nil {
		return "", "", err
	}

	for _, serie := range series {
		if sanitizeFilename(serie.GetContent().Title) == contentTitle {
			for _, episode := range serie.Episodes {
				if episode.Path == nil {
					continue
				}
				
				path := *episode.Path
				episodeFilename := filepath.Base(path)
				
				// Check if filename matches
				if episodeFilename == filename {
					return serie.GetContent().ResourceID, path, nil
				}
				
				// Check generated filename
				if episode.Season != nil && episode.Episode != nil {
					generatedName := fmt.Sprintf("S%02dE%02d%s", *episode.Season, *episode.Episode, getFileExtension(path))
					if generatedName == filename {
						return serie.GetContent().ResourceID, path, nil
					}
				}
			}
		}
	}

	return "", "", nil
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

func getContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".flv":
		return "video/x-flv"
	case ".webm":
		return "video/webm"
	case ".m4v":
		return "video/x-m4v"
	default:
		return "application/octet-stream"
	}
}
