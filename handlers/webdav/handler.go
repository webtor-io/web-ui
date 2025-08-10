package webdav

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
)

type Handler struct {
	pg     *cs.PG
	at     *at.AccessToken
	domain string
	sapi   *api.Api
}

func RegisterHandler(c *cli.Context, r *gin.Engine, pg *cs.PG, at *at.AccessToken, sapi *api.Api) {
	h := &Handler{
		pg:     pg,
		at:     at,
		domain: c.String("domain"),
		sapi:   sapi,
	}

	gr := r.Group("/webdav")
	gr.Use(auth.HasAuth)
	gr.Use(claims.IsPaid)
	gr.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "PUT", "DELETE", "PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK"},
		AllowHeaders: []string{"*"},
	}))
	gr.POST("/url/generate", h.generateUrl)

	// WebDAV API endpoints with token authentication
	grapi := gr.Group("")
	grapi.Use(at.HasScope("webdav:read"))
	grapi.Handle("PROPFIND", "/*path", h.propfind)
	grapi.GET("/*path", h.get)

	// Write operations require webdav:write scope
	grwrite := gr.Group("")
	grwrite.Use(at.HasScope("webdav:write"))
	grwrite.PUT("/*path", h.put)
	grwrite.DELETE("/*path", h.delete)
	grwrite.Handle("MKCOL", "/*path", h.mkcol)
}

func (s *Handler) generateUrl(c *gin.Context) {
	_, err := s.at.Generate(c, "webdav", []string{"webdav:read", "webdav:write"})
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) propfind(c *gin.Context) {
	requestPath := strings.TrimPrefix(c.Param("path"), "/")
	
	// Parse PROPFIND request body
	var propfind PropFind
	if c.Request.ContentLength > 0 {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}
		if err := xml.Unmarshal(body, &propfind); err != nil {
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}
	}

	// Get depth header (0 = current resource only, 1 = current + children, infinity = all)
	depth := c.GetHeader("Depth")
	if depth == "" {
		depth = "infinity"
	}

	responses, err := s.buildPropfindResponse(c, requestPath, depth)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	multistatus := Multistatus{
		Responses: responses,
	}

	c.Header("Content-Type", "application/xml; charset=utf-8")
	c.XML(http.StatusMultiStatus, multistatus)
}

func (s *Handler) get(c *gin.Context) {
	requestPath := strings.TrimPrefix(c.Param("path"), "/")
	
	if requestPath == "" {
		// Root directory - return directory listing as HTML for browsers
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, s.generateRootHTML())
		return
	}

	// Parse path to determine folder type and file
	parts := strings.SplitN(requestPath, "/", 2)
	folderType := FolderType(parts[0])
	
	switch folderType {
	case FolderTypeTorrents:
		if len(parts) == 1 {
			// Directory listing for torrents folder
			c.AbortWithStatus(http.StatusMethodNotAllowed)
			return
		}
		// Handle torrent file download
		s.handleTorrentFileDownload(c, parts[1])
	case FolderTypeAll, FolderTypeMovies, FolderTypeSeries:
		if len(parts) == 1 {
			// Directory listing - not supported for GET
			c.AbortWithStatus(http.StatusMethodNotAllowed)
			return
		}
		// Handle content file download
		s.handleContentFileDownload(c, folderType, parts[1])
	default:
		c.AbortWithStatus(http.StatusNotFound)
	}
}

func (s *Handler) put(c *gin.Context) {
	requestPath := strings.TrimPrefix(c.Param("path"), "/")
	
	// Only allow PUT in torrents directory
	parts := strings.SplitN(requestPath, "/", 2)
	if len(parts) < 2 || FolderType(parts[0]) != FolderTypeTorrents {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	filename := parts[1]
	if !strings.HasSuffix(strings.ToLower(filename), ".torrent") {
		c.AbortWithStatus(http.StatusUnsupportedMediaType)
		return
	}

	// Read torrent file content
	_, err := io.ReadAll(c.Request.Body)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	// TODO: Implement torrent file upload to library
	// This would involve parsing the torrent file and adding it to the user's library
	
	c.Status(http.StatusCreated)
}

func (s *Handler) delete(c *gin.Context) {
	requestPath := strings.TrimPrefix(c.Param("path"), "/")
	
	// Only allow DELETE in torrents directory
	parts := strings.SplitN(requestPath, "/", 2)
	if len(parts) < 2 || FolderType(parts[0]) != FolderTypeTorrents {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	// TODO: Implement torrent removal from library
	
	c.Status(http.StatusNoContent)
}

func (s *Handler) mkcol(c *gin.Context) {
	// MKCOL is not supported - all directories are virtual
	c.AbortWithStatus(http.StatusMethodNotAllowed)
}

func (s *Handler) buildPropfindResponse(c *gin.Context, requestPath string, depth string) ([]Response, error) {
	var responses []Response
	
	if requestPath == "" {
		// Root directory
		response := s.createDirectoryResponse("/", "WebDAV Root")
		responses = append(responses, response)
		
		if depth != "0" {
			// Add main folders
			folders := []struct {
				path string
				name string
			}{
				{"/torrents/", "Torrents"},
				{"/all/", "All Content"},
				{"/movies/", "Movies"},
				{"/series/", "Series"},
			}
			
			for _, folder := range folders {
				response := s.createDirectoryResponse(folder.path, folder.name)
				responses = append(responses, response)
			}
		}
		
		return responses, nil
	}

	// Parse path to determine folder type
	parts := strings.SplitN(requestPath, "/", 2)
	folderType := FolderType(parts[0])
	
	switch folderType {
	case FolderTypeTorrents:
		return s.buildTorrentsResponse(c, requestPath, depth)
	case FolderTypeAll:
		return s.buildAllContentResponse(c, requestPath, depth)
	case FolderTypeMovies:
		return s.buildMoviesResponse(c, requestPath, depth)
	case FolderTypeSeries:
		return s.buildSeriesResponse(c, requestPath, depth)
	default:
		return nil, errors.New("folder not found")
	}
}

func (s *Handler) createDirectoryResponse(href, displayName string) Response {
	return Response{
		Href: href,
		Propstat: Propstat{
			Prop: Prop{
				DisplayName:  &displayName,
				ResourceType: &ResourceType{Collection: &Collection{}},
				LastModified: stringPtr(time.Now().Format(time.RFC1123)),
				CreationDate: stringPtr(time.Now().Format(time.RFC3339)),
			},
			Status: "HTTP/1.1 200 OK",
		},
	}
}

func (s *Handler) createFileResponse(href, displayName string, size int64, contentType, modTime string) Response {
	return Response{
		Href: href,
		Propstat: Propstat{
			Prop: Prop{
				DisplayName:   &displayName,
				ResourceType:  &ResourceType{}, // Empty for files
				ContentLength: &size,
				ContentType:   &contentType,
				LastModified:  &modTime,
				CreationDate:  &modTime,
			},
			Status: "HTTP/1.1 200 OK",
		},
	}
}

func (s *Handler) buildTorrentsResponse(c *gin.Context, requestPath string, depth string) ([]Response, error) {
	var responses []Response
	
	// Add the torrents directory itself
	response := s.createDirectoryResponse("/"+requestPath, "Torrents")
	responses = append(responses, response)
	
	if depth == "0" {
		return responses, nil
	}

	// Get user's torrent library
	ctx := c.Request.Context()
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	// Get all torrents from library (both movies and series)
	movies, err := models.GetLibraryMovieList(ctx, db, u.ID, models.SortTypeRecentlyAdded)
	if err != nil {
		return nil, err
	}

	series, err := models.GetLibrarySeriesList(ctx, db, u.ID, models.SortTypeRecentlyAdded)
	if err != nil {
		return nil, err
	}

	// Add movie torrents
	for _, movie := range movies {
		filename := fmt.Sprintf("%s.torrent", movie.GetContent().Title)
		href := fmt.Sprintf("/%s/%s", requestPath, filename)
		size := int64(1024) // Placeholder size for torrent files
		modTime := time.Now().Format(time.RFC1123)
		
		response := s.createFileResponse(href, filename, size, "application/x-bittorrent", modTime)
		responses = append(responses, response)
	}

	// Add series torrents
	for _, serie := range series {
		filename := fmt.Sprintf("%s.torrent", serie.GetContent().Title)
		href := fmt.Sprintf("/%s/%s", requestPath, filename)
		size := int64(1024) // Placeholder size for torrent files
		modTime := time.Now().Format(time.RFC1123)
		
		response := s.createFileResponse(href, filename, size, "application/x-bittorrent", modTime)
		responses = append(responses, response)
	}

	return responses, nil
}

// These functions are now implemented in content.go

func (s *Handler) handleTorrentFileDownload(c *gin.Context, filename string) {
	// TODO: Implement torrent file download
	c.AbortWithStatus(http.StatusNotImplemented)
}

// This function is now implemented in content.go

func (s *Handler) generateRootHTML() string {
	return `<!DOCTYPE html>
<html>
<head>
    <title>WebDAV Root</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        .folder { margin: 10px 0; }
        .folder a { text-decoration: none; color: #0066cc; }
        .folder a:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <h1>WebDAV Root Directory</h1>
    <p>Available folders:</p>
    <div class="folder">📁 <a href="/webdav/torrents/">torrents/</a> - Manage your torrent library</div>
    <div class="folder">📁 <a href="/webdav/all/">all/</a> - All content (read-only)</div>
    <div class="folder">📁 <a href="/webdav/movies/">movies/</a> - Movies only (read-only)</div>
    <div class="folder">📁 <a href="/webdav/series/">series/</a> - Series only (read-only)</div>
</body>
</html>`
}

func (s *Handler) retrieveTorrentItem(ctx context.Context, hash string, claims *api.Claims, path string) (*ra.ListItem, error) {
	limit := uint(100)
	offset := uint(0)
	for {
		resp, err := s.sapi.ListResourceContent(ctx, claims, hash, &api.ListResourceContentArgs{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range resp.Items {
			if item.PathStr == path {
				return &item, nil
			}
		}
		if (resp.Count - int(offset)) == len(resp.Items) {
			break
		}
		offset += limit
	}
	return nil, nil
}

func stringPtr(s string) *string {
	return &s
}
