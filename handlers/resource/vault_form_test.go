package resource

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/urfave/cli"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/vault"
)

// The pledge modal asks for what the pledge will take. Without a
// vault.resource row that is the upload's finished size: the pledge seals
// the upload first, and what is reserved after the seal is its size, however
// much an abandoned part declared before it. With a row, the row's
// required_vp, which the pledge compares the points with, for any resource.
func TestUploadPledgeFormRequired(t *testing.T) {
	db := pgtest.Start(t)
	pgCS := pgtest.CS(t, db)
	o := newUploadPageUser(t, db)
	if _, err := db.Exec(`INSERT INTO vault.user_vp (user_id, total) VALUES (?, NULL)`, o); err != nil {
		t.Fatal(err)
	}
	const gib = 1 << 30
	for n, c := range []struct {
		name    string
		torrent bool
		row     float64 // the vault.resource row's required_vp, 0: no row
		over    map[string]any
		want    []string
	}{
		{"a part declared", false, 0, map[string]any{"size": gib, "files_count": 1, "reserved": 9 * gib},
			[]string{"[required=1.00]", "[pledgeEmpty=false][pledgeIncomplete=true]"}},
		{"no file finished", false, 0, map[string]any{"size": 0, "files_count": 0, "reserved": 5 * gib},
			[]string{"[required=0.00]", "[pledgeEmpty=true][pledgeIncomplete=false]"}},
		{"a row", false, 3, map[string]any{"size": gib, "files_count": 1, "reserved": gib},
			[]string{"[required=3.00]"}},
		{"a torrent with a row", true, 2, map[string]any{"size": 2 * gib, "files_count": 1, "private": false, "reserved": nil},
			[]string{"[required=2.00]", "[pledgeEmpty=false][pledgeIncomplete=false]"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			id := uploadID(0x600 + n)
			if c.torrent {
				id = intentHash
			}
			if c.row != 0 {
				if _, err := db.Exec(`INSERT INTO vault.resource (resource_id, required_vp, funded_vp, funded, name) VALUES (?, ?, 0, false, 'n')`, id, c.row); err != nil {
					t.Fatal(err)
				}
			}
			rest := viewAPI(t, c.over)
			ru, _ := url.Parse(rest.URL)
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			for _, f := range api.RegisterFlags(nil) {
				f.Apply(fs)
			}
			if err := fs.Parse([]string{"--webtor-rest-api-host=" + ru.Hostname(), "--webtor-rest-api-port=" + ru.Port(),
				"--rapidapi-host=", "--rapidapi-key="}); err != nil {
				t.Fatal(err)
			}
			cc := cli.NewContext(cli.NewApp(), fs, nil)
			v := vault.New(cc, &vault.Api{}, claims.New(nil, nil, nil), http.DefaultClient, pgCS, api.New(cc, http.DefaultClient))
			r := newResourceRouterPG(t, rest, v, &models.User{UserID: o}, pgCS)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/"+id+"?pledge-form=true", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status %d, body %q", w.Code, w.Body.String())
			}
			for _, want := range c.want {
				if !strings.Contains(w.Body.String(), want) {
					t.Errorf("body %q, want %s", w.Body.String(), want)
				}
			}
		})
	}
}

// uploadPledgeState tells the modal an upload has no finished file, or has a
// part declared beyond its finished ones; a torrent and an upload of another
// kind are neither.
func TestUploadPledgeState(t *testing.T) {
	const gib int64 = 1 << 30
	res := func(v int64) *int64 { return &v }
	for _, c := range []struct {
		name              string
		r                 *ra.ResourceResponse
		empty, incomplete bool
	}{
		{"nil", nil, false, false},
		{"a part declared, no file finished", &ra.ResourceResponse{ID: pledgeUpload, FilesCount: 0, Reserved: res(5 * gib)}, true, false},
		{"empty", &ra.ResourceResponse{ID: pledgeUpload, FilesCount: 0, Reserved: res(0)}, true, false},
		{"a part declared", &ra.ResourceResponse{ID: pledgeUpload, FilesCount: 2, Size: gib, Reserved: res(9 * gib)}, false, true},
		{"all finished", &ra.ResourceResponse{ID: pledgeUpload, FilesCount: 2, Size: gib, Reserved: res(gib)}, false, false},
		{"no reserved", &ra.ResourceResponse{ID: pledgeUpload, FilesCount: 2, Size: gib}, false, false},
		{"a torrent", &ra.ResourceResponse{ID: intentHash, FilesCount: 0}, false, false},
		{"kind 1", &ra.ResourceResponse{ID: kind1, FilesCount: 0}, false, false},
	} {
		if empty, incomplete := uploadPledgeState(c.r); empty != c.empty || incomplete != c.incomplete {
			t.Errorf("%s: %v, %v; want %v, %v", c.name, empty, incomplete, c.empty, c.incomplete)
		}
	}
}
