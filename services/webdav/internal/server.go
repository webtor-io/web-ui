package internal

import (
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func ServeError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		code = httpErr.Code
	}

	var errElt *Error
	if errors.As(err, &errElt) {
		w.WriteHeader(code)
		ServeXML(w).Encode(errElt)
		return
	}

	http.Error(w, err.Error(), code)
}

// DecodeXMLRequest decodes the request body as an XML document.
//
// Per RFC 4918 the body of a WebDAV method like PROPFIND/PROPPATCH is an XML
// document, but the spec does not require the client to send a particular
// Content-Type. Some widely-used clients (notably rclone) send a valid XML
// body with no Content-Type header at all, so we must not gate decoding on it
// — doing so previously broke `rclone about` (Statfs) and listing with a
// "webdav: unsupported request body" 400. This mirrors the tolerant behaviour
// of golang.org/x/net/webdav.
func DecodeXMLRequest(r *http.Request, v interface{}) error {
	if err := xml.NewDecoder(r.Body).Decode(v); err != nil {
		return &HTTPError{http.StatusBadRequest, err}
	}
	return nil
}

func ServeXML(w http.ResponseWriter) *xml.Encoder {
	w.Header().Add("Content-Type", "application/xml; charset=\"utf-8\"")
	w.Write([]byte(xml.Header))
	return xml.NewEncoder(w)
}

func ServeMultiStatus(w http.ResponseWriter, ms *MultiStatus) error {
	// TODO: streaming
	w.WriteHeader(http.StatusMultiStatus)
	return ServeXML(w).Encode(ms)
}

type Backend interface {
	Options(r *http.Request) (caps []string, allow []string, err error)
	HeadGet(w http.ResponseWriter, r *http.Request) error
	PropFind(r *http.Request, pf *PropFind, depth Depth) (*MultiStatus, error)
	PropPatch(r *http.Request, pu *PropertyUpdate) (*Response, error)
	Put(w http.ResponseWriter, r *http.Request) error
	Delete(r *http.Request) error
	Mkcol(r *http.Request) error
	Copy(r *http.Request, dest *Href, recursive, overwrite bool) (created bool, err error)
	Move(r *http.Request, dest *Href, overwrite bool) (created bool, err error)
}

type Handler struct {
	Backend Backend
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var err error
	if h.Backend == nil {
		err = fmt.Errorf("webdav: no backend available")
	} else {
		switch r.Method {
		case http.MethodOptions:
			err = h.handleOptions(w, r)
		case http.MethodGet, http.MethodHead:
			err = h.Backend.HeadGet(w, r)
		case http.MethodPut:
			err = h.Backend.Put(w, r)
		case http.MethodDelete:
			// TODO: send a multistatus in case of partial failure
			err = h.Backend.Delete(r)
			if err == nil {
				w.WriteHeader(http.StatusNoContent)
			}
		case "PROPFIND":
			err = h.handlePropfind(w, r)
		case "PROPPATCH":
			err = h.handleProppatch(w, r)
		case "MKCOL":
			err = h.Backend.Mkcol(r)
			if err == nil {
				w.WriteHeader(http.StatusCreated)
			}
		case "COPY", "MOVE":
			err = h.handleCopyMove(w, r)
		default:
			err = HTTPErrorf(http.StatusMethodNotAllowed, "webdav: unsupported method")
		}
	}

	if err != nil {
		ServeError(w, err)
	}
}

func (h *Handler) handleOptions(w http.ResponseWriter, r *http.Request) error {
	caps, allow, err := h.Backend.Options(r)
	if err != nil {
		return err
	}
	caps = append([]string{"1", "3"}, caps...)

	w.Header().Add("DAV", strings.Join(caps, ", "))
	w.Header().Add("Allow", strings.Join(allow, ", "))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) handlePropfind(w http.ResponseWriter, r *http.Request) error {
	var propfind PropFind

	// An empty body means allprop (RFC 4918 §9.1). Otherwise the body is an
	// XML document, which we parse regardless of Content-Type — see
	// DecodeXMLRequest for why we can't rely on the header.
	body := bufio.NewReader(r.Body)
	if _, err := body.Peek(1); err == io.EOF {
		propfind.AllProp = &struct{}{}
	} else if err := xml.NewDecoder(body).Decode(&propfind); err != nil {
		return &HTTPError{http.StatusBadRequest, err}
	}

	depth := DepthInfinity
	if s := r.Header.Get("Depth"); s != "" {
		var err error
		depth, err = ParseDepth(s)
		if err != nil {
			return &HTTPError{http.StatusBadRequest, err}
		}
	}

	ms, err := h.Backend.PropFind(r, &propfind, depth)
	if err != nil {
		return err
	}

	return ServeMultiStatus(w, ms)
}

type PropFindFunc func(raw *RawXMLValue) (interface{}, error)

func PropFindValue(value interface{}) PropFindFunc {
	return func(raw *RawXMLValue) (interface{}, error) {
		return value, nil
	}
}

func NewPropFindResponse(path string, propfind *PropFind, props map[xml.Name]PropFindFunc) (*Response, error) {
	resp := &Response{Hrefs: []Href{Href{Path: path}}}

	if _, ok := props[ResourceTypeName]; !ok {
		props[ResourceTypeName] = PropFindValue(NewResourceType())
	}

	if propfind.PropName != nil {
		for xmlName, _ := range props {
			emptyVal := NewRawXMLElement(xmlName, nil, nil)
			if err := resp.EncodeProp(http.StatusOK, emptyVal); err != nil {
				return nil, err
			}
		}
	} else if propfind.AllProp != nil {
		// TODO: add support for propfind.Include
		for xmlName, f := range props {
			emptyVal := NewRawXMLElement(xmlName, nil, nil)

			val, err := f(emptyVal)

			code := http.StatusOK
			if err != nil {
				// TODO: don't throw away error message here
				code = HTTPErrorFromError(err).Code
				val = emptyVal
			}

			if err := resp.EncodeProp(code, val); err != nil {
				return nil, err
			}
		}
	} else if prop := propfind.Prop; prop != nil {
		// Emit the properties we can satisfy first, and defer the 404
		// propstat for requested-but-unknown properties to the end.
		//
		// RFC 4918 lets a response carry several <propstat> blocks with
		// different statuses and doesn't mandate an order, but some widely
		// used clients only look at the status of the *first* <propstat> and
		// discard the whole entry if it isn't 2xx — notably rclone in
		// owncloud/nextcloud mode (Prop.StatusOK reads Status[0]). rclone
		// always asks for <displayname> (which we don't expose) before
		// <resourcetype>, so emitting the 404 block first made every entry
		// look failed and listings came back empty. Returning the found
		// props first mirrors golang.org/x/net/webdav and keeps both lenient
		// and strict clients happy.
		var notFound []xml.Name
		for _, raw := range prop.Raw {
			xmlName, ok := raw.XMLName()
			if !ok {
				continue
			}

			f, ok := props[xmlName]
			if !ok {
				notFound = append(notFound, xmlName)
				continue
			}

			emptyVal := NewRawXMLElement(xmlName, nil, nil)

			code := http.StatusOK
			var val interface{} = emptyVal
			if v, err := f(&raw); err != nil {
				// TODO: don't throw away error message here
				code = HTTPErrorFromError(err).Code
			} else {
				val = v
			}

			if err := resp.EncodeProp(code, val); err != nil {
				return nil, err
			}
		}

		for _, xmlName := range notFound {
			emptyVal := NewRawXMLElement(xmlName, nil, nil)
			if err := resp.EncodeProp(http.StatusNotFound, emptyVal); err != nil {
				return nil, err
			}
		}
	} else {
		return nil, HTTPErrorf(http.StatusBadRequest, "webdav: request missing propname, allprop or prop element")
	}

	return resp, nil
}

func (h *Handler) handleProppatch(w http.ResponseWriter, r *http.Request) error {
	var update PropertyUpdate
	if err := DecodeXMLRequest(r, &update); err != nil {
		return err
	}

	resp, err := h.Backend.PropPatch(r, &update)
	if err != nil {
		return err
	}

	ms := NewMultiStatus(*resp)
	return ServeMultiStatus(w, ms)
}

func parseDestination(h http.Header) (*Href, error) {
	destHref := h.Get("Destination")
	if destHref == "" {
		return nil, HTTPErrorf(http.StatusBadRequest, "webdav: missing Destination header in MOVE request")
	}
	dest, err := url.Parse(destHref)
	if err != nil {
		return nil, HTTPErrorf(http.StatusBadRequest, "webdav: marlformed Destination header in MOVE request: %v", err)
	}
	return (*Href)(dest), nil
}

func (h *Handler) handleCopyMove(w http.ResponseWriter, r *http.Request) error {
	dest, err := parseDestination(r.Header)
	if err != nil {
		return err
	}

	overwrite := true
	if s := r.Header.Get("Overwrite"); s != "" {
		overwrite, err = ParseOverwrite(s)
		if err != nil {
			return err
		}
	}

	depth := DepthInfinity
	if s := r.Header.Get("Depth"); s != "" {
		depth, err = ParseDepth(s)
		if err != nil {
			return err
		}
	}

	var created bool
	if r.Method == "COPY" {
		var recursive bool
		switch depth {
		case DepthZero:
			recursive = false
		case DepthOne:
			return HTTPErrorf(http.StatusBadRequest, `webdav: "Depth: 1" is not supported in COPY request`)
		case DepthInfinity:
			recursive = true
		}

		created, err = h.Backend.Copy(r, dest, recursive, overwrite)
	} else {
		if depth != DepthInfinity {
			return HTTPErrorf(http.StatusBadRequest, `webdav: only "Depth: infinity" is accepted in MOVE request`)
		}
		created, err = h.Backend.Move(r, dest, overwrite)
	}
	if err != nil {
		return err
	}

	if created {
		w.WriteHeader(http.StatusCreated)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
	return nil
}
