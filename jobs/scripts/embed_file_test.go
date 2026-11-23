package scripts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/web"
)

// raQuery hands a request's query to rest-api's own argument parser.
type raQuery url.Values

func (q raQuery) Param(string) string          { return "" }
func (q raQuery) Query(k string) string        { return url.Values(q).Get(k) }
func (q raQuery) QueryArray(k string) []string { return url.Values(q)[k] }
func (q raQuery) GetHeader(string) string      { return "" }

// The embed's file or path is found by its full path. Matching names in a
// listing of the directory saw only that listing's first page of 10 items.
// /list is answered by rest-api's own listing code over the files below.
func TestEmbedFindsRequestedFile(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	// One top directory: 12 episodes, then a subdirectory with a file of
	// the same name as the last of them.
	var files []*ra.File
	for n := 1; n <= 12; n++ {
		files = append(files, &ra.File{Path: []string{"Show", fmt.Sprintf("e%02d.mkv", n)}, Size: 1})
	}
	files = append(files,
		&ra.File{Path: []string{"Show", "Extras", "e12.mkv"}, Size: 1},
		&ra.File{Path: []string{"Show", "Extras", "trailer.mkv"}, Size: 1})
	res := &ra.Resource{ID: hash, Files: files}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/resource/"+hash+"/list" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		args, err := ra.ListGetArgsFromParams(raQuery(r.URL.Query()))
		if err != nil {
			t.Errorf("list args: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		l, _ := ra.NewList().Get(res, args)
		_ = json.NewEncoder(w).Encode(l)
	}))
	defer srv.Close()
	ls := testLoadScript(t, srv, &api.Claims{})

	cases := []struct {
		name     string
		settings models.EmbedSettings
		want     string // the item's path; empty: an error
	}{
		{name: "path, 11th in its directory", settings: models.EmbedSettings{Path: "Show/e11.mkv"}, want: "/Show/e11.mkv"},
		{name: "path with leading slash", settings: models.EmbedSettings{Path: "/Show/e12.mkv"}, want: "/Show/e12.mkv"},
		{name: "pwd and file, 12th", settings: models.EmbedSettings{PWD: "Show", File: "e12.mkv"}, want: "/Show/e12.mkv"},
		{name: "nested directory", settings: models.EmbedSettings{Path: "Show/Extras/e12.mkv"}, want: "/Show/Extras/e12.mkv"},
		{name: "file under the single top directory", settings: models.EmbedSettings{File: "e02.mkv"}, want: "/Show/e02.mkv"},
		{name: "file under the single top directory, past 10", settings: models.EmbedSettings{File: "e12.mkv"}, want: "/Show/e12.mkv"},
		{name: "same name in another directory", settings: models.EmbedSettings{PWD: "Show/Extras", File: "e05.mkv"}},
		{name: "a directory", settings: models.EmbedSettings{Path: "Show/Extras"}},
		{name: "below a file", settings: models.EmbedSettings{Path: "Show/e01.mkv/e01.mkv"}},
		{name: "missing", settings: models.EmbedSettings{Path: "Show/e99.mkv"}},
		{name: "nothing requested", settings: models.EmbedSettings{}, want: "/Show/e01.mkv"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &EmbedScript{api: ls.api, i18n: ls.i18n, c: &web.Context{Lang: "en", ApiClaims: &api.Claims{}}}
			j := job.New(context.Background(), "t", "test", nil, &job.NilStorage{}, false, nil)
			settings := tc.settings
			i, err := s.getBestItem(context.Background(), j, hash, &settings)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("got %+v, want an error", i)
				}
				if !strings.Contains(err.Error(), "requested file not found") {
					t.Errorf("error %q, want the reason", err)
				}
				for _, name := range []string{"Show", "Extras", ".mkv"} {
					if strings.Contains(err.Error(), name) {
						t.Errorf("error %q names the request", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("getBestItem: %v", err)
			}
			if i.PathStr != tc.want || i.Type != ra.ListTypeFile {
				t.Errorf("got %s %s, want file %s", i.Type, i.PathStr, tc.want)
			}
		})
	}
}
