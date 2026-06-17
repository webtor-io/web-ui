package internal

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeBackend records the arguments PROPFIND/PROPPATCH were decoded into so
// tests can assert how the request body was parsed.
type fakeBackend struct {
	propFindCalled bool
	lastPropFind   *PropFind
	lastDepth      Depth

	propPatchCalled bool
	lastUpdate      *PropertyUpdate
}

func (b *fakeBackend) Options(r *http.Request) ([]string, []string, error) {
	return nil, []string{"OPTIONS", "PROPFIND"}, nil
}
func (b *fakeBackend) HeadGet(w http.ResponseWriter, r *http.Request) error { return nil }
func (b *fakeBackend) PropFind(r *http.Request, pf *PropFind, depth Depth) (*MultiStatus, error) {
	b.propFindCalled = true
	b.lastPropFind = pf
	b.lastDepth = depth
	return NewMultiStatus(Response{Hrefs: []Href{{Path: "/"}}}), nil
}
func (b *fakeBackend) PropPatch(r *http.Request, pu *PropertyUpdate) (*Response, error) {
	b.propPatchCalled = true
	b.lastUpdate = pu
	return &Response{Hrefs: []Href{{Path: "/"}}}, nil
}
func (b *fakeBackend) Put(w http.ResponseWriter, r *http.Request) error { return nil }
func (b *fakeBackend) Delete(r *http.Request) error                     { return nil }
func (b *fakeBackend) Mkcol(r *http.Request) error                      { return nil }
func (b *fakeBackend) Copy(r *http.Request, dest *Href, recursive, overwrite bool) (bool, error) {
	return false, nil
}
func (b *fakeBackend) Move(r *http.Request, dest *Href, overwrite bool) (bool, error) {
	return false, nil
}

func do(t *testing.T, method, body, contentType, depth string) (*httptest.ResponseRecorder, *fakeBackend) {
	t.Helper()
	be := &fakeBackend{}
	h := &Handler{Backend: be}

	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "/", http.NoBody)
	} else {
		r = httptest.NewRequest(method, "/", strings.NewReader(body))
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if depth != "" {
		r.Header.Set("Depth", depth)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w, be
}

// The exact body `rclone about` (Statfs) sends, with NO Content-Type header.
// This is the regression that produced
// "400 Bad Request: webdav: unsupported request body".
const rcloneQuotaBody = `<?xml version="1.0" ?>
<D:propfind xmlns:D="DAV:">
 <D:prop>
  <D:quota-available-bytes/>
  <D:quota-used-bytes/>
 </D:prop>
</D:propfind>`

func TestPropfind_BodyWithoutContentType_rcloneStatfs(t *testing.T) {
	w, be := do(t, "PROPFIND", rcloneQuotaBody, "" /* no Content-Type */, "0")

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207 Multi-Status, got %d: %s", w.Code, w.Body.String())
	}
	if !be.propFindCalled {
		t.Fatal("backend.PropFind was not called")
	}
	if be.lastPropFind.Prop == nil {
		t.Fatal("expected the quota <prop> body to be parsed, got nil Prop")
	}
	if be.lastPropFind.AllProp != nil {
		t.Fatal("body was present, must not be treated as allprop")
	}
	if be.lastDepth != DepthZero {
		t.Fatalf("expected Depth 0, got %v", be.lastDepth)
	}
}

func TestPropfind_BodyWithoutContentType_listing(t *testing.T) {
	// Some rclone vendors send an explicit prop body for directory listing too.
	body := `<?xml version="1.0"?><D:propfind xmlns:D="DAV:"><D:prop><D:displayname/><D:getlastmodified/></D:prop></D:propfind>`
	w, be := do(t, "PROPFIND", body, "" /* no Content-Type */, "1")

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207 Multi-Status, got %d: %s", w.Code, w.Body.String())
	}
	if be.lastPropFind.Prop == nil {
		t.Fatal("expected <prop> body to be parsed")
	}
	if be.lastDepth != DepthOne {
		t.Fatalf("expected Depth 1, got %v", be.lastDepth)
	}
}

func TestPropfind_EmptyBody_isAllprop(t *testing.T) {
	for _, ct := range []string{"", "text/xml", "application/octet-stream"} {
		w, be := do(t, "PROPFIND", "" /* empty body */, ct, "0")
		if w.Code != http.StatusMultiStatus {
			t.Fatalf("ct=%q: expected 207, got %d: %s", ct, w.Code, w.Body.String())
		}
		if be.lastPropFind.AllProp == nil {
			t.Fatalf("ct=%q: empty body must be treated as allprop", ct)
		}
	}
}

func TestPropfind_WithXMLContentType(t *testing.T) {
	w, be := do(t, "PROPFIND", rcloneQuotaBody, "text/xml; charset=utf-8", "0")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d: %s", w.Code, w.Body.String())
	}
	if be.lastPropFind.Prop == nil {
		t.Fatal("expected <prop> body to be parsed")
	}
}

func TestPropfind_MalformedBody_is400(t *testing.T) {
	w, be := do(t, "PROPFIND", "<this is not valid xml", "", "0")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed XML, got %d: %s", w.Code, w.Body.String())
	}
	if be.propFindCalled {
		t.Fatal("backend.PropFind must not be called for a malformed body")
	}
}

// rclone in owncloud/nextcloud mode only inspects the status of the *first*
// <propstat> of each <response> (Prop.StatusOK reads Status[0]) and drops the
// whole entry if it isn't 2xx. It asks for <displayname> (which we don't
// expose) before <resourcetype>, so the found props must be emitted before the
// 404 block or every listed item is silently discarded and `ls` shows nothing.
func TestNewPropFindResponse_FoundPropsBeforeNotFound(t *testing.T) {
	// Order mirrors rclone's owncloud request: an unsupported prop first,
	// the one we satisfy (resourcetype) afterwards.
	propfind := &PropFind{
		Prop: &Prop{Raw: xmlNamesToRaw([]xml.Name{
			{Space: Namespace, Local: "displayname"},
			ResourceTypeName,
		})},
	}

	props := map[xml.Name]PropFindFunc{
		ResourceTypeName: PropFindValue(NewResourceType(CollectionName)),
	}

	resp, err := NewPropFindResponse("/all/", propfind, props)
	if err != nil {
		t.Fatalf("NewPropFindResponse: %v", err)
	}

	if len(resp.PropStats) != 2 {
		t.Fatalf("expected 2 propstats (200 + 404), got %d", len(resp.PropStats))
	}
	if got := resp.PropStats[0].Status.Code; got != http.StatusOK {
		t.Fatalf("first propstat must be 200 so rclone owncloud-mode keeps the item, got %d", got)
	}
	if got := resp.PropStats[1].Status.Code; got != http.StatusNotFound {
		t.Fatalf("second propstat must be 404, got %d", got)
	}
}

func TestProppatch_BodyWithoutContentType(t *testing.T) {
	body := `<?xml version="1.0"?><D:propertyupdate xmlns:D="DAV:"><D:set><D:prop><D:getlastmodified>Mon, 01 Jun 2026 00:00:00 GMT</D:getlastmodified></D:prop></D:set></D:propertyupdate>`
	w, be := do(t, "PROPPATCH", body, "" /* no Content-Type */, "")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d: %s", w.Code, w.Body.String())
	}
	if !be.propPatchCalled {
		t.Fatal("backend.PropPatch was not called")
	}
}
