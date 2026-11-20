package enrich

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/urfave/cli"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/services/api"
)

// Enrichment reads the listing for the shared media-info row, whoever started
// it: as internal, never as the user who started the load or the library add.
func TestRetrieveTorrentItemsReadsAsInternal(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	var claims jwt.MapClaims
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/resource/"+hash+"/list" {
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		p, err := jwt.Parse(r.Header.Get("X-Token"), func(*jwt.Token) (interface{}, error) { return []byte("test"), nil })
		if err != nil || !p.Valid {
			t.Errorf("X-Token did not validate: %v", err)
		} else {
			claims = p.Claims.(jwt.MapClaims)
		}
		_, _ = w.Write([]byte(`{"items":[],"items_count":0}`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	e := &Enricher{api: api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client())}

	if _, err := e.retrieveTorrentItems(context.Background(), hash); err != nil {
		t.Fatal(err)
	}
	if claims["internal"] != true {
		t.Errorf("internal = %v, want true; claims %+v", claims["internal"], claims)
	}
	if _, hasUid := claims["uid"]; hasUid {
		t.Errorf("the listing was read as a user: %+v", claims)
	}
}

// An upload can shrink while enrichment walks its listing. rest-api answers an
// offset past the end with 200, no items and the current count; the walk ends
// there instead of asking for the next page until its deadline.
func TestRetrieveTorrentItemsStopsWhenTheListingShrinks(t *testing.T) {
	const hash = "9c5db5752b7c0123456789abcdef0123456789ab"
	var mu sync.Mutex
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/resource/"+hash+"/list" {
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		switch {
		case n == 1:
			items := make([]ra.ListItem, 100)
			for i := range items {
				items[i] = ra.ListItem{Type: ra.ListTypeFile, Name: fmt.Sprintf("f%d.txt", i), PathStr: fmt.Sprintf("/Name/f%d.txt", i), Index: i}
			}
			b, _ := json.Marshal(ra.ListResponse{Items: items, Count: 250})
			_, _ = w.Write(b)
		case n <= 3:
			_, _ = w.Write([]byte(`{"items":[],"items_count":50}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	e := &Enricher{api: api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client())}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	items, err := e.retrieveTorrentItems(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 100 {
		t.Errorf("%d items, want 100", len(items))
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 2 {
		t.Errorf("%d list requests, want 2", requests)
	}
}
