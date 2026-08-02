package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	restapi "github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/libapi"
	"github.com/webtor-io/web-ui/services/web"
)

// listLibrary godoc
//
//	@Summary		List your library
//	@Description	The torrents saved to the account, newest first by default.
//	@Description
//	@Description	`type` filters the same way the library tabs do: `movies` and `series` mean a torrent with at least
//	@Description	one recognized film or show, which is decided by enrichment — a torrent added a moment ago may not
//	@Description	be classified yet and shows up only under `all`.
//	@Tags			library
//	@Produce		json
//	@Security		BearerAuth
//	@Param			type	query		string	false	"Filter"	Enums(all, movies, series)	default(all)
//	@Param			sort	query		string	false	"Sort order"	Enums(recent, name)		default(recent)
//	@Param			limit	query		int		false	"Page size (default 100, max 1000)"
//	@Param			offset	query		int		false	"Page offset"
//	@Success		200		{object}	libapi.LibraryListResponse
//	@Failure		400		{object}	libapi.ErrorResponse
//	@Failure		401		{object}	libapi.ErrorResponse
//	@Failure		402		{object}	libapi.ErrorResponse
//	@Router			/library [get]
func (s *Handler) listLibrary(c *gin.Context) {
	typ := c.DefaultQuery("type", libapi.LibraryTypeAll)
	switch typ {
	case libapi.LibraryTypeAll, libapi.LibraryTypeMovies, libapi.LibraryTypeSeries:
	default:
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, "type must be all, movies or series", nil))
		return
	}
	sort := c.DefaultQuery("sort", libapi.LibrarySortRecent)
	switch sort {
	case libapi.LibrarySortRecent, libapi.LibrarySortName:
	default:
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, "sort must be recent or name", nil))
		return
	}
	limit, err := uintQuery(c, "limit")
	if err != nil {
		s.abort(c, err)
		return
	}
	offset, err := uintQuery(c, "offset")
	if err != nil {
		s.abort(c, err)
		return
	}

	db, err := s.db()
	if err != nil {
		s.abort(c, err)
		return
	}
	u := auth.GetUserFromContext(c)
	rows, lerr := loadLibrary(c.Request.Context(), db, u.ID, typ, sort)
	if lerr != nil {
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to load the library", lerr))
		return
	}

	res := &libapi.LibraryListResponse{
		Items:  []libapi.LibraryItem{},
		Count:  len(rows),
		Limit:  pageLimit(int(limit)),
		Offset: int(offset),
		Type:   typ,
		Sort:   sort,
	}
	// Paged in memory: the underlying queries are the library UI's, which read
	// the whole list to render counts, and a library big enough for that to
	// matter does not exist yet. Worth revisiting the day it does.
	for i := res.Offset; i < len(rows) && len(res.Items) < res.Limit; i++ {
		res.Items = append(res.Items, libapi.NewLibraryItem(rows[i]))
	}
	c.JSON(http.StatusOK, res)
}

// getLibraryItem godoc
//
//	@Summary		Get one library entry
//	@Description	Answers `404` when the resource is not in your library, which is how a client checks membership
//	@Description	before offering to add or remove it.
//	@Tags			library
//	@Produce		json
//	@Security		BearerAuth
//	@Param			resource_id	path		string	true	"Infohash"	example(08ada5a7a6183aae1e09d831df6748d566095a10)
//	@Success		200			{object}	libapi.LibraryItem
//	@Failure		401			{object}	libapi.ErrorResponse
//	@Failure		402			{object}	libapi.ErrorResponse
//	@Failure		404			{object}	libapi.ErrorResponse
//	@Router			/library/{resource_id} [get]
func (s *Handler) getLibraryItem(c *gin.Context) {
	l, err := s.libraryItem(c)
	if err != nil {
		s.abort(c, err)
		return
	}
	c.JSON(http.StatusOK, libapi.NewLibraryItem(l))
}

// addLibrary godoc
//
//	@Summary		Add a torrent to your library
//	@Description	Takes a resource id that is already in the store — `POST /resource` puts it there and returns one.
//	@Description
//	@Description	Idempotent: adding a resource already in the library answers `200` with the existing entry, and only
//	@Description	a real insert answers `201`. Adding also queues
//	@Description	enrichment (title, year, poster, movie/series classification), which runs in the background — the
//	@Description	entry is usable immediately but may not answer to `type=movies` for a little while.
//	@Tags			library
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		libapi.LibraryAddRequest	true	"Resource to add"
//	@Success		200		{object}	libapi.LibraryItem			"Already in the library"
//	@Success		201		{object}	libapi.LibraryItem
//	@Failure		400		{object}	libapi.ErrorResponse
//	@Failure		401		{object}	libapi.ErrorResponse
//	@Failure		402		{object}	libapi.ErrorResponse
//	@Failure		404		{object}	libapi.ErrorResponse	"The resource is not in the store"
//	@Router			/library [post]
func (s *Handler) addLibrary(c *gin.Context) {
	var req libapi.LibraryAddRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest,
			"body must be a JSON object with \"resource_id\"", err))
		return
	}
	rID := normalizeResourceID(req.ResourceID)
	if rID == "" {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, "resource_id is required", nil))
		return
	}
	db, err := s.db()
	if err != nil {
		s.abort(c, err)
		return
	}
	u := auth.GetUserFromContext(c)

	// Already there: answer 200 with the existing entry rather than 201. The
	// insert below is ON CONFLICT DO NOTHING, so a retry would otherwise report
	// "created" for something it did not create — and clients branch on that.
	existing, derr := models.GetLibraryByResourceID(c.Request.Context(), db, u.ID, rID)
	if derr != nil {
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to check the library", derr))
		return
	}
	if existing != nil {
		c.JSON(http.StatusOK, libapi.NewLibraryItem(existing))
		return
	}

	// The torrent is fetched and parsed rather than trusted from the request:
	// the library row carries name, size and file count, and those have to come
	// from the metainfo the store actually holds. Same path the UI's add
	// button takes.
	t, aerr := s.api.GetTorrentCached(c.Request.Context(), restapi.GetClaimsFromContext(c), rID)
	if aerr != nil {
		s.abort(c, upstreamError(aerr, "failed to fetch the torrent"))
		return
	}
	if len(t) == 0 {
		s.abort(c, notFound("no such resource in the store — POST /resource first"))
		return
	}
	mi, merr := metainfo.Load(io.NopCloser(bytes.NewReader(t)))
	if merr != nil {
		s.abort(c, libapi.NewError(http.StatusBadGateway, libapi.CodeUpstream, "the stored torrent is malformed", merr))
		return
	}
	info, merr := mi.UnmarshalInfo()
	if merr != nil {
		s.abort(c, libapi.NewError(http.StatusBadGateway, libapi.CodeUpstream, "the stored torrent is malformed", merr))
		return
	}
	if _, derr = models.AddTorrentToLibrary(c.Request.Context(), db, u.ID, rID, &info, "", int64(len(t))); derr != nil {
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to add to the library", derr))
		return
	}
	if s.jobs != nil {
		_, _ = s.jobs.Enrich(web.NewContext(c), rID)
	}
	// Re-read rather than return what the insert built: that struct carries no
	// Torrent relation, so size and file count would go out as zeros — which
	// reads as "an empty torrent was added", not as "not loaded".
	created, derr := models.GetLibraryByResourceID(c.Request.Context(), db, u.ID, rID)
	if derr != nil || created == nil {
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal,
			"the entry was added but could not be read back", derr))
		return
	}
	c.JSON(http.StatusCreated, libapi.NewLibraryItem(created))
}

// renameLibrary godoc
//
//	@Summary		Rename a library entry
//	@Description	Changes the name the item is shown under everywhere — the library, WebDAV and S3 all read this
//	@Description	row. It does not touch the torrent itself.
//	@Tags			library
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			resource_id	path		string						true	"Infohash"	example(08ada5a7a6183aae1e09d831df6748d566095a10)
//	@Param			body		body		libapi.LibraryRenameRequest	true	"New name"
//	@Success		200			{object}	libapi.LibraryItem
//	@Failure		400			{object}	libapi.ErrorResponse
//	@Failure		401			{object}	libapi.ErrorResponse
//	@Failure		402			{object}	libapi.ErrorResponse
//	@Failure		404			{object}	libapi.ErrorResponse
//	@Router			/library/{resource_id} [patch]
func (s *Handler) renameLibrary(c *gin.Context) {
	var req libapi.LibraryRenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest,
			"body must be a JSON object with \"name\"", err))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, "name must not be empty", nil))
		return
	}
	l, err := s.libraryItem(c)
	if err != nil {
		s.abort(c, err)
		return
	}
	db, err := s.db()
	if err != nil {
		s.abort(c, err)
		return
	}
	l.Name = name
	if derr := models.UpdateLibraryName(c.Request.Context(), db, l); derr != nil {
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to rename", derr))
		return
	}
	c.JSON(http.StatusOK, libapi.NewLibraryItem(l))
}

// deleteLibrary godoc
//
//	@Summary		Remove a torrent from your library
//	@Description	Removes the entry from your library. The resource itself stays in the store and keeps working —
//	@Description	this is not a takedown, and a Vault pledge on it is a separate thing (`DELETE /vault/pledges`).
//	@Description
//	@Description	Not idempotent: removing something that is not there answers `404`.
//	@Tags			library
//	@Produce		json
//	@Security		BearerAuth
//	@Param			resource_id	path	string	true	"Infohash"	example(08ada5a7a6183aae1e09d831df6748d566095a10)
//	@Success		204			"Removed"
//	@Failure		401			{object}	libapi.ErrorResponse
//	@Failure		402			{object}	libapi.ErrorResponse
//	@Failure		404			{object}	libapi.ErrorResponse
//	@Router			/library/{resource_id} [delete]
func (s *Handler) deleteLibrary(c *gin.Context) {
	if _, err := s.libraryItem(c); err != nil {
		s.abort(c, err)
		return
	}
	db, err := s.db()
	if err != nil {
		s.abort(c, err)
		return
	}
	u := auth.GetUserFromContext(c)
	if derr := models.RemoveFromLibrary(c.Request.Context(), db, u.ID, normalizeResourceID(c.Param("resource_id"))); derr != nil {
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to remove from the library", derr))
		return
	}
	c.Status(http.StatusNoContent)
}

// libraryItem resolves the path parameter to a row the caller owns, so every
// handler that acts on one gets the same 404 for "not yours" as for "not
// there" — the difference is not the caller's business.
func (s *Handler) libraryItem(c *gin.Context) (*models.Library, *libapi.Error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	u := auth.GetUserFromContext(c)
	l, derr := models.GetLibraryByResourceID(c.Request.Context(), db, u.ID, normalizeResourceID(c.Param("resource_id")))
	if derr != nil {
		return nil, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to load the library entry", derr)
	}
	if l == nil {
		return nil, notFound("this resource is not in your library")
	}
	return l, nil
}

func (s *Handler) db() (*pg.DB, *libapi.Error) {
	db := s.pg.Get()
	if db == nil {
		return nil, libapi.NewError(http.StatusServiceUnavailable, libapi.CodeUnavailable, "database is not available", nil)
	}
	return db, nil
}

func loadLibrary(ctx context.Context, db *pg.DB, userID uuid.UUID, typ string, sort string) ([]*models.Library, error) {
	st := models.SortTypeRecentlyAdded
	if sort == libapi.LibrarySortName {
		st = models.SortTypeName
	}
	switch typ {
	case libapi.LibraryTypeMovies:
		return models.GetLibraryMovieTorrentList(ctx, db, userID, st)
	case libapi.LibraryTypeSeries:
		return models.GetLibrarySeriesTorrentList(ctx, db, userID, st)
	default:
		return models.GetLibraryTorrentsList(ctx, db, userID, st)
	}
}

const (
	defaultPageLimit = 100
	maxPageLimit     = 1000
)

func pageLimit(v int) int {
	if v <= 0 {
		return defaultPageLimit
	}
	return min(v, maxPageLimit)
}
