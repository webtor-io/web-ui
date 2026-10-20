package resource

import (
	"context"
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
)

// A directory with one file is shown as that file -- but not under ?file=,
// whose links reload only #list. Support 1c56d0b1: "Version 5.0" of a series
// pack went blank when opened with an episode of "Version 4.0" selected.
func TestPrepareGetData_SingleFileDirUnderFileKeepsList(t *testing.T) {
	const (
		hash = "9e6fb4038530a10a88294a0cfef5900d13378a92"
		dir  = "/Pack/Version 5.0"
		ep5  = dir + "/S05.mp4"
		ep4  = "/Pack/Version 4.0/S04E06.mp4"
	)
	file := func(p string) ra.ListItem {
		return ra.ListItem{ID: p, Name: p, PathStr: p, Type: ra.ListTypeFile, MediaFormat: ra.Video}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v any
		switch r.URL.Path {
		case "/resource/" + hash:
			v = ra.ResourceResponse{ID: hash, Name: "Pack", MultiFile: true}
		case "/resource/" + hash + "/list":
			p := r.URL.Query().Get("path")
			if p == dir {
				p = ep5
			}
			v = ra.ListResponse{Items: []ra.ListItem{file(p)}, Count: 1}
		default:
			t.Errorf("unexpected rest-api call %s", r.URL)
		}
		_ = json.NewEncoder(w).Encode(v)
	}))
	t.Cleanup(srv.Close)

	u, _ := url.Parse(srv.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range append(api.RegisterFlags(nil), cs.RegisterPGFlags(nil)...) {
		f.Apply(fs)
	}
	// Explicit values for everything the environment could fill in; no
	// Postgres host leaves the page anonymous and DB-less.
	if err := fs.Parse([]string{
		"--webtor-rest-api-host", host, "--webtor-rest-api-port", port, "--webtor-rest-api-secure=false",
		"--rapidapi-host", "", "--rapidapi-key", "", "--postgres-host", "",
	}); err != nil {
		t.Fatal(err)
	}
	c := cli.NewContext(cli.NewApp(), fs, nil)
	s := &Handler{api: api.New(c, srv.Client()), pg: cs.NewPG(c)}

	for _, tc := range []struct {
		name, file, item string
		list             bool
	}{
		{"no file: collapses into its file", "", ep5, false},
		{"file in another directory", ep4, ep4, true},
		{"file is its only entry", ep5, ep5, true},
	} {
		d, err := s.prepareGetData(context.Background(), &GetArgs{
			ID: hash, Page: 1, PageSize: pageSize, PWD: dir, File: tc.file, User: &auth.User{},
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if d.Item == nil || d.Item.PathStr != tc.item {
			t.Errorf("%s: item = %+v, want %s", tc.name, d.Item, tc.item)
		}
		if got := d.List != nil; got != tc.list {
			t.Errorf("%s: list shown = %v, want %v", tc.name, got, tc.list)
		}
	}
}
