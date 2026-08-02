package api

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ra "github.com/webtor-io/rest-api/services"
	restapi "github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/libapi"
)

// maxResourceBody bounds a POST /resource body. A .torrent is metadata, not
// content — anything past a few MB is not one, and reading it into memory to
// find that out is what we are avoiding.
const maxResourceBody = 8 << 20

// postResource godoc
//
//	@Summary		Store a resource
//	@Description	Takes a `.torrent` file or a magnet URI in the request body and puts it in the store, returning the
//	@Description	resource id (its infohash) that every other endpoint takes. A magnet is resolved against the
//	@Description	BitTorrent network first, which can take minutes for a torrent nobody is seeding.
//	@Description
//	@Description	Storing does **not** add the torrent to your library — `POST /library` does, and takes the id this
//	@Description	returns.
//	@Description
//	@Description	Identical in shape to rest-api's `POST /resource/`.
//	@Tags			resource
//	@Accept			*/*
//	@Produce		json
//	@Security		BearerAuth
//	@Param			resource	body		string	true	"Raw .torrent bytes, or a magnet URI"	example(magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10)
//	@Success		200			{object}	services.ResourceResponse
//	@Failure		400			{object}	libapi.ErrorResponse
//	@Failure		401			{object}	libapi.ErrorResponse
//	@Failure		402			{object}	libapi.ErrorResponse
//	@Failure		404			{object}	libapi.ErrorResponse	"The torrent could not be fetched"
//	@Failure		408			{object}	libapi.ErrorResponse	"A magnet took too long to resolve"
//	@Router			/resource [post]
func (s *Handler) postResource(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxResourceBody+1))
	if err != nil {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, "failed to read the request body", err))
		return
	}
	if len(body) == 0 {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest,
			"the body must be a .torrent file or a magnet uri", nil))
		return
	}
	if len(body) > maxResourceBody {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, "the body is too large for a torrent", nil))
		return
	}
	res, aerr := s.api.StoreResource(c.Request.Context(), restapi.GetClaimsFromContext(c), body)
	if aerr != nil {
		s.abort(c, upstreamError(aerr, "failed to store the resource"))
		return
	}
	if res == nil {
		// nil here means upstream answered 404 — the client turns that into a
		// zero value, not an error. For a store request that is "we could not
		// get this torrent"; a genuine fetch timeout arrives through the error
		// path above as 408.
		s.abort(c, notFound("could not fetch this torrent from the BitTorrent network"))
		return
	}
	c.PureJSON(http.StatusOK, res)
}

// getResource godoc
//
//	@Summary		Get a resource
//	@Description	Name, size, file count and magnet URI of a stored resource. Single-file torrents also carry the
//	@Description	file itself, so the common case needs no `/list` round-trip.
//	@Description
//	@Description	Appending `.torrent` to the id returns the torrent file instead
//	@Description	(`GET /resource/{resource_id}.torrent`, `application/x-bittorrent`) — same convention as rest-api.
//	@Tags			resource
//	@Produce		json
//	@Security		BearerAuth
//	@Param			resource_id	path		string	true	"Infohash, optionally suffixed with .torrent"	example(08ada5a7a6183aae1e09d831df6748d566095a10)
//	@Success		200			{object}	services.ResourceResponse
//	@Failure		401			{object}	libapi.ErrorResponse
//	@Failure		402			{object}	libapi.ErrorResponse
//	@Failure		404			{object}	libapi.ErrorResponse
//	@Router			/resource/{resource_id} [get]
func (s *Handler) getResource(c *gin.Context) {
	id := normalizeResourceID(c.Param("resource_id"))
	// rest-api routes the torrent download off the same path, and a client
	// that learned the convention there must find it here too.
	if strings.HasSuffix(id, ".torrent") {
		s.getResourceTorrent(c, strings.TrimSuffix(id, ".torrent"))
		return
	}
	res, err := s.api.GetResource(c.Request.Context(), restapi.GetClaimsFromContext(c), id)
	if err != nil {
		s.abort(c, upstreamError(err, "failed to get the resource"))
		return
	}
	if res == nil {
		s.abort(c, notFound("no such resource"))
		return
	}
	c.PureJSON(http.StatusOK, res)
}

func (s *Handler) getResourceTorrent(c *gin.Context, id string) {
	t, err := s.api.GetTorrentCached(c.Request.Context(), restapi.GetClaimsFromContext(c), id)
	if err != nil {
		s.abort(c, upstreamError(err, "failed to get the torrent"))
		return
	}
	if len(t) == 0 {
		s.abort(c, notFound("no such resource"))
		return
	}
	c.Data(http.StatusOK, "application/x-bittorrent", t)
}

// listResource godoc
//
//	@Summary		List resource contents
//	@Description	Files and directories of a resource. The `id` of each item is what `/export` takes.
//	@Description
//	@Description	`output=tree` lists one directory level at a time (use `path` to descend); `output=list` returns the
//	@Description	flat file list. Identical in shape to rest-api's `/resource/{id}/list`, including the `items` /
//	@Description	`items_count` envelope.
//	@Tags			list
//	@Produce		json
//	@Security		BearerAuth
//	@Param			resource_id	path		string	true	"Infohash"	example(08ada5a7a6183aae1e09d831df6748d566095a10)
//	@Param			path		query		string	false	"Directory to list (with output=tree)"	default(/)
//	@Param			limit		query		int		false	"Page size"								default(10)
//	@Param			offset		query		int		false	"Page offset"
//	@Param			output		query		string	false	"Listing shape"							Enums(list, tree)	default(list)
//	@Param			sort		query		string	false	"Sort order"							Enums(name, size)	default(name)
//	@Success		200			{object}	services.ListResponse
//	@Failure		400			{object}	libapi.ErrorResponse
//	@Failure		401			{object}	libapi.ErrorResponse
//	@Failure		402			{object}	libapi.ErrorResponse
//	@Failure		404			{object}	libapi.ErrorResponse
//	@Router			/resource/{resource_id}/list [get]
func (s *Handler) listResource(c *gin.Context) {
	args := &restapi.ListResourceContentArgs{
		Path: c.Query("path"),
		Sort: c.Query("sort"),
	}
	switch out := c.Query("output"); out {
	case "":
	case string(restapi.OutputList), string(restapi.OutputTree):
		args.Output = restapi.ListResourceContentOutputType(out)
	default:
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, "output must be list or tree", nil))
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
	args.Limit, args.Offset = limit, offset

	id := normalizeResourceID(c.Param("resource_id"))
	claims := restapi.GetClaimsFromContext(c)
	res, aerr := s.api.ListResourceContent(c.Request.Context(), claims, id, args)
	if aerr != nil {
		s.abort(c, upstreamError(aerr, "failed to list the resource"))
		return
	}
	if res == nil {
		s.abort(c, notFound("no such resource"))
		return
	}
	// A resource that does not exist reaches us as an *empty listing*, not as
	// an error: the client turns upstream's 404 into a zero value. Answering
	// 200 with an empty `items` for a typo'd infohash would be
	// indistinguishable from an empty directory, so confirm the resource is
	// real before saying so. The check runs only on the empty path and hits a
	// cache, so the common case pays nothing.
	if res.Count == 0 && len(res.Items) == 0 {
		r, rerr := s.api.GetResourceCached(c.Request.Context(), claims, id)
		if rerr != nil {
			s.abort(c, upstreamError(rerr, "failed to list the resource"))
			return
		}
		if r == nil {
			s.abort(c, notFound("no such resource"))
			return
		}
	}
	c.PureJSON(http.StatusOK, res)
}

// exportResource godoc
//
//	@Summary		Export a file
//	@Description	Returns the URLs a file is served from: `download` for the bytes, `stream` for the player sources
//	@Description	and subtitle tracks, plus the auxiliary outputs. Same shape as rest-api's
//	@Description	`/resource/{id}/export/{content_id}`.
//	@Description
//	@Description	`content_id` is either the `id` from `/list` or the file's index in the torrent's natural order.
//	@Description	Exporting a directory produces an archive URL — `archive-format` picks zip or tar, and `paths`
//	@Description	limits it to a selection.
//	@Description
//	@Description	**The URLs carry their own authorization and are short-lived.** Resolve them when you are about to
//	@Description	use them; do not store them.
//	@Tags			export
//	@Produce		json
//	@Security		BearerAuth
//	@Param			resource_id		path		string		true	"Infohash"		example(08ada5a7a6183aae1e09d831df6748d566095a10)
//	@Param			content_id		path		string		true	"File id from /list, or its index"	example(ca2453df3e7691c28934eebed5a253ee0aabd29f)
//	@Param			output			query		string		false	"Return only this export"			Enums(download, stream, torrent_client_stat, subtitles, media_probe)
//	@Param			imdb-id			query		string		false	"IMDb id, used to match subtitles"
//	@Param			archive-format	query		string		false	"Archive format for directory exports"	Enums(zip, tar)	default(zip)
//	@Param			paths			query		[]string	false	"Limit a directory archive to these paths (repeatable)"
//	@Success		200				{object}	services.ExportResponse
//	@Failure		400				{object}	libapi.ErrorResponse
//	@Failure		401				{object}	libapi.ErrorResponse
//	@Failure		402				{object}	libapi.ErrorResponse
//	@Failure		404				{object}	libapi.ErrorResponse
//	@Router			/resource/{resource_id}/export/{content_id} [get]
func (s *Handler) exportResource(c *gin.Context) {
	format := c.Query("archive-format")
	switch format {
	case "", "zip", "tar":
	default:
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, "archive-format must be zip or tar", nil))
		return
	}
	res, err := s.api.ExportResourceContentWithArchiveFormat(
		c.Request.Context(), restapi.GetClaimsFromContext(c),
		normalizeResourceID(c.Param("resource_id")), normalizeResourceID(c.Param("content_id")),
		c.Query("imdb-id"), format, c.QueryArray("paths"),
	)
	if err != nil {
		s.abort(c, upstreamError(err, "failed to export the file"))
		return
	}
	if res == nil || len(res.ExportItems) == 0 {
		s.abort(c, notFound("no such resource or file"))
		return
	}
	// `output` filters the response here rather than upstream: the client we
	// proxy through always asks for the full set, and every export it can
	// return is already in hand by the time we get here.
	if out := c.Query("output"); out != "" {
		item, ok := res.ExportItems[out]
		if !ok {
			s.abort(c, notFound("this file has no \""+out+"\" export"))
			return
		}
		res = &ra.ExportResponse{Source: res.Source, ExportItems: map[string]ra.ExportItem{out: item}}
	}
	c.PureJSON(http.StatusOK, res)
}

// upstreamError turns a rest-api client failure into an API error.
//
// The client we proxy through discards the upstream status and hands back a
// plain error carrying rest-api's message, so the message is the only signal
// left. Classifying it by substring is not elegant — but it is what rest-api's
// own error middleware does to pick that status in the first place
// (`services/web.go`), so matching the same substrings reproduces its decision
// rather than inventing a second, divergent one. The alternative, guessing 502
// for everything, would report "the torrent does not exist" as "webtor is
// broken".
func upstreamError(err error, message string) *libapi.Error {
	e := strings.ToLower(err.Error())
	switch {
	case strings.Contains(e, "forbidden"):
		return libapi.NewError(http.StatusForbidden, libapi.CodeForbidden,
			"your plan does not allow this operation on this resource", err)
	case strings.Contains(e, "not found"):
		return libapi.NewError(http.StatusNotFound, libapi.CodeNotFound, "no such resource", err)
	case strings.Contains(e, "failed to parse"):
		return libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest,
			"the request could not be parsed", err)
	case strings.Contains(e, "timeout"):
		return libapi.NewError(http.StatusRequestTimeout, libapi.CodeUpstreamTimeout,
			"the torrent could not be fetched in time — try again later", err)
	case strings.Contains(e, "deadline exceeded"):
		return libapi.NewError(http.StatusGatewayTimeout, libapi.CodeUpstreamTimeout,
			"an upstream service ran out of time — try again later", err)
	}
	return libapi.NewError(http.StatusBadGateway, libapi.CodeUpstream, message, err)
}

func notFound(message string) *libapi.Error {
	return libapi.NewError(http.StatusNotFound, libapi.CodeNotFound, message, nil)
}

// normalizeResourceID lower-cases an infohash. rest-api does the same on every
// path that takes one, so a client that upper-cases it would otherwise address
// the same torrent under two identities — one in the store and the library,
// another in the Vault — and a delete issued in the other case would miss.
func normalizeResourceID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

// uintQuery reads a non-negative integer query parameter. A malformed one is a
// client bug, not a reason to silently fall back to a default: a typo'd
// `limit=1o` that quietly returns 10 items reads as "the API ignores me".
func uintQuery(c *gin.Context, name string) (uint, *libapi.Error) {
	v := c.Query(name)
	if v == "" {
		return 0, nil
	}
	i, err := strconv.Atoi(v)
	if err != nil || i < 0 {
		return 0, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, name+" must be a non-negative integer", err)
	}
	return uint(i), nil
}
