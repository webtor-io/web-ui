package resource

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urfave/cli"
	proto "github.com/webtor-io/claims-provider/proto"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/vault"
)

const kind1 = "9c5db5752b7c1123456789abcdef0123456789ab"

// openTag is the opening tag that holds marker: from the last "<" before
// marker's first appearance to the first ">" after it.
func openTag(t *testing.T, html, marker string) string {
	t.Helper()
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("no %q in the page", marker)
	}
	return html[strings.LastIndex(html[:i], "<") : i+strings.Index(html[i:], ">")+1]
}

// uploadTemplates are the resource templates with uploads on or off: the
// zero web.Helper has them off.
func uploadTemplates(t *testing.T, enabled bool) *template.Template {
	t.Helper()
	return resourceTemplates(t).Funcs(template.FuncMap{"uploadsEnabled": func() bool { return enabled }})
}

// uploadPageData is the two-file page of intentPage as the owner's upload.
func uploadPageData(up *UploadView) *GetData {
	gd := intentPage("", true)
	gd.Args.ID, gd.Resource.ID = uploadU, uploadU
	gd.Upload = up
	return gd
}

func checkPage(t *testing.T, html string, in, out []string) {
	t.Helper()
	for _, s := range in {
		if !strings.Contains(html, s) {
			t.Errorf("no %q", s)
		}
	}
	for _, s := range out {
		if strings.Contains(html, s) {
			t.Errorf("%q is there", s)
		}
	}
}

func TestUploadView(t *testing.T) {
	const (
		busy       = `data-busy="Wait until your files finish uploading"`
		deleteForm = `action="/upload/delete"`
		// "Delete all" names what goes; the Vault sentence only where a copy
		// may be kept: the upload is not writable.
		confirmFull  = `onclick="return confirm('Delete “Name” and all its files for good?')"`
		confirmVault = `onclick="return confirm('Delete “Name” and all its files for good? Its Vault copy will be deleted too.')"`
		confirmPart  = `return confirm('Delete “`
	)
	writable := func() *UploadView { return &UploadView{IsOwner: true, CanWrite: true, Name: "Name"} }
	render := func(t *testing.T, enabled bool, gd *GetData, edit func(*GetData)) string {
		t.Helper()
		if edit != nil {
			edit(gd)
		}
		return execute(t, uploadTemplates(t, enabled), "main", pageContext("en", gd))
	}

	t.Run("render", func(t *testing.T) {
		gd := uploadPageData(writable())
		gd.Args.PWD = "/Name/sub"
		file := gd.List.Items[1].PathStr
		html := render(t, true, gd, nil)
		checkPage(t, html, []string{`data-upload-add`, `data-upload-id="` + uploadU + `"`, `data-upload-name="Name"`, `data-upload-dir="/Name/sub"`,
			`</button><input type="file" multiple hidden>`, `Delete all`, confirmFull, `data-upload-delete="` + file + `"`},
			[]string{`data-upload-upsell`, `data-upload-root`, `Its Vault copy`})
		if tag := openTag(t, html, `data-upload-add`); !strings.Contains(tag, busy) {
			t.Errorf("add files: %s", tag)
		}
		// The log of what "Add files" sends, under the header, above the
		// upload's state line.
		if tag := openTag(t, html, `data-upload-log`); !strings.Contains(tag, `data-async-target="main"`) || !strings.Contains(tag, `class="hidden progress-alert`) {
			t.Errorf("upload log: %s", tag)
		}
		if i, j := strings.Index(html, `data-upload-log`), strings.Index(html, `data-upload-state`); j < 0 || i > j {
			t.Errorf("the upload log at %d, the state line at %d", i, j)
		}
		if tag := openTag(t, html, `data-upload-delete="`+file+`"`); !strings.Contains(tag, busy) {
			t.Errorf("delete file: %s", tag)
		}
		if tag := openTag(t, html, confirmPart); !strings.HasPrefix(tag, `<button type="submit"`) {
			t.Errorf("the dialog is not on the submit button: %s", tag)
		}
	})

	t.Run("a free account", func(t *testing.T) {
		gd := uploadPageData(writable())
		ctx := pageContext("en", gd)
		ctx.Claims = &claims.Data{Context: &proto.Context{Tier: &proto.Tier{Id: 0}}, Claims: &proto.Claims{Site: &proto.Site{NoAds: true}}}
		checkPage(t, execute(t, uploadTemplates(t, true), "main", ctx), []string{`data-upload-upsell`}, nil)
	})

	t.Run("vault", func(t *testing.T) {
		html := render(t, true, uploadPageData(writable()), func(gd *GetData) { gd.Vault = true })
		if tag := openTag(t, html, `data-upload-add`); !strings.Contains(tag, `data-upload-vault`) {
			t.Errorf("add files: %s", tag)
		}
		html = render(t, true, uploadPageData(writable()), func(gd *GetData) { gd.Vault = false })
		checkPage(t, html, []string{`data-upload-add`}, []string{`data-upload-vault`})
	})

	for _, c := range []struct {
		name    string
		up      *UploadView
		enabled bool
		in, out []string
	}{
		{"saved to Vault", &UploadView{IsOwner: true, Name: "Name"}, true, []string{`Delete all`, deleteForm, confirmVault}, []string{`data-upload-add`, `data-upload-delete`, `data-upload-log`}},
		{"saved and paid", &UploadView{IsOwner: true, Saved: true, Name: "Name"}, true, []string{`Delete all`, deleteForm, confirmVault}, []string{`data-upload-add`, `data-upload-log`}},
		{"not an upload", nil, true, nil, []string{`data-upload-`, `Delete all`, `return confirm(`}},
		{"uploads off", writable(), false, nil, []string{`data-upload-add`, `data-upload-delete`, deleteForm, `Delete all`, `data-upload-log`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			checkPage(t, render(t, c.enabled, uploadPageData(c.up), nil), c.in, c.out)
		})
	}

	t.Run("a detected movie", func(t *testing.T) {
		html := render(t, true, uploadPageData(writable()), func(gd *GetData) {
			gd.Movie = &models.Movie{
				VideoContent:  &models.VideoContent{ResourceID: uploadU, Title: "Sample Movie"},
				MovieMetadata: &models.MovieMetadata{VideoMetadata: &models.VideoMetadata{VideoID: "tt0000001", Title: "Sample Movie"}},
			}
		})
		checkPage(t, html, []string{`Delete all`, confirmPart}, nil)
		for _, s := range []string{`data-upload-add`, deleteForm} {
			if n := strings.Count(html, s); n != 1 {
				t.Errorf("%q %d times, want once", s, n)
			}
		}
	})

	t.Run("folders", func(t *testing.T) {
		// The listing of the upload's root folder with a folder added; the
		// paths come from the fixture.
		withDir := func(writable bool) (*GetData, string) {
			gd := uploadPageData(nil)
			gd.Upload = &UploadView{IsOwner: true, CanWrite: writable, Name: gd.Resource.Name}
			dir := gd.List.PathStr + "/dir"
			gd.List.Items = append(gd.List.Items, ra.ListItem{ID: "dir-x", Name: "dir", PathStr: dir, Type: ra.ListTypeDirectory})
			gd.List.Count = 3
			gd.Item = &gd.List.Items[0]
			return gd, dir
		}
		list := func(t *testing.T, enabled bool, gd *GetData) string {
			t.Helper()
			return execute(t, uploadTemplates(t, enabled), "list", pageContext("en", gd))
		}
		none := []string{`data-upload-delete`, `data-confirm`}

		for _, open := range []bool{false, true} {
			gd, dir := withDir(true)
			if open {
				gd.Args.File = gd.List.Items[0].PathStr
			}
			html := list(t, true, gd)
			checkPage(t, html, []string{`data-upload-delete="` + dir + `/"`, `data-confirm="Delete the folder “dir” and every file in it? This cannot be undone."`, `aria-label="Delete folder"`}, nil)
			if tag := openTag(t, html, `data-upload-delete="`+dir+`/"`); !strings.Contains(tag, busy) {
				t.Errorf("a file open %v: delete folder: %s", open, tag)
			}
		}

		gd, _ := withDir(false)
		checkPage(t, list(t, true, gd), nil, none)

		// The listing of "/": the upload's root folder alone.
		gd, _ = withDir(true)
		gd.List = &ra.ListResponse{ListItem: ra.ListItem{PathStr: "/", Type: ra.ListTypeDirectory}, Items: []ra.ListItem{intentPage("", true).List.ListItem}, Count: 1}
		gd.Item = nil
		checkPage(t, list(t, true, gd), nil, none)

		gd, _ = withDir(true)
		checkPage(t, list(t, false, gd), nil, none)
	})

	t.Run("page", testUploadViewPage)
}

// viewAPI answers /resource/<id> with body, the class's answer with what
// over replaces, and the listing under it: the one root folder, and in it
// files_count files that sum to size.
func viewAPI(t *testing.T, over map[string]any) *httptest.Server {
	t.Helper()
	body := map[string]any{"name": "n", "multi_file": true, "size": 0, "files_count": 0, "private": true, "reserved": 0}
	for k, v := range over {
		body[k] = v
	}
	size, count := int64(body["size"].(int)), body["files_count"].(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, list := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/resource/"), "/list")
		w.Header().Set("Content-Type", "application/json")
		if !list {
			b := map[string]any{"id": id}
			for k, v := range body {
				b[k] = v
			}
			_ = json.NewEncoder(w).Encode(b)
			return
		}
		items := []ra.ListItem{}
		switch path := r.URL.Query().Get("path"); {
		case count == 0:
		case path == "/":
			items = append(items, ra.ListItem{Type: ra.ListTypeDirectory, Name: "n", PathStr: "/n"})
		default:
			for i := range count {
				s := size / int64(count)
				if i == count-1 {
					s = size - s*int64(count-1)
				}
				name := fmt.Sprintf("f%d.bin", i)
				items = append(items, ra.ListItem{Type: ra.ListTypeFile, Name: name, PathStr: path + "/" + name, Index: i, Size: s})
			}
		}
		_ = json.NewEncoder(w).Encode(ra.ListResponse{Items: items, Count: len(items)})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testUploadViewPage(t *testing.T) {
	db := pgtest.Start(t)
	pgCS := pgtest.CS(t, db)
	o := newUploadPageUser(t, db)
	if _, err := db.Exec(`INSERT INTO vault.user_vp (user_id, total) VALUES (?, 10)`, o); err != nil {
		t.Fatal(err)
	}
	exec := func(t *testing.T, q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	const mib = 1 << 20
	const (
		noVault  = iota // the router has no Vault
		vaultOn         // Vault on the page's database
		vaultOff        // Vault on a connection closed before the request
	)
	ready := map[string]any{"size": mib, "files_count": 2, "reserved": mib}
	declared := map[string]any{"size": mib, "files_count": 2, "reserved": 17 * mib}

	n := 0
	for _, c := range []struct {
		name         string
		kind1        bool
		over         map[string]any
		row          string // vault.resource: "" none, "bare" without a pledge, "paid" funded with o's funded pledge, "unpaid" neither funded
		vault        int
		renamePledge bool
		want         []string
	}{
		{"not in Vault", false, nil, "", vaultOn, false, []string{"[upload=true/true/false]", "[unknown=false]", "[unpaid=false]"}},
		{"refused by VP", false, nil, "bare", vaultOn, false, []string{"[upload=true/false/false]", "[unknown=false]", "[unpaid=false]"}},
		{"saved", false, nil, "paid", vaultOn, false, []string{"[upload=true/false/true]", "[unknown=false]", "[unpaid=false]"}},
		{"the plan ended", false, nil, "unpaid", vaultOn, false, []string{"[upload=true/false/false]", "[unknown=false]", "[unpaid=true]"}},
		{"kind 1", true, nil, "", vaultOn, false, []string{"[upload=]", "[incomplete=]"}},
		{"Vault down, saved", false, nil, "paid", vaultOff, false, []string{"[upload=true/false/false]", "[unknown=true]"}},
		{"Vault down, not in Vault", false, nil, "", vaultOff, false, []string{"[upload=true/false/false]", "[unknown=true]"}},
		{"pledges unreadable, saved", false, nil, "paid", vaultOn, true, []string{"[upload=true/false/false]", "[unknown=true]"}},
		{"pledges unreadable, the plan ended", false, nil, "unpaid", vaultOn, true, []string{"[unpaid=false]", "[unknown=true]"}},
		{"no Vault", false, nil, "", noVault, false, []string{"[upload=true/true/false]", "[unknown=false]", "[unpaid=false]"}},
		{"all done", false, ready, "", vaultOn, false, []string{"[incomplete=false]"}},
		{"a part declared", false, declared, "", vaultOn, false, []string{"[incomplete=true]"}},
		{"no file done", false, nil, "", vaultOn, false, []string{"[incomplete=true]"}},
		{"a part declared, in Vault", false, declared, "bare", vaultOn, false, []string{"[incomplete=false]"}},
		{"no name yet", false, map[string]any{"name": ""}, "", vaultOn, false, []string{"[uname=Name]"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			n++
			id := uploadID(0x500 + n)
			if c.kind1 {
				id = kind1
			}
			switch c.row {
			case "bare":
				exec(t, `INSERT INTO vault.resource (resource_id, required_vp, funded_vp, funded, name) VALUES (?, 1, 0, false, 'n')`, id)
			case "paid":
				exec(t, `INSERT INTO vault.resource (resource_id, required_vp, funded_vp, funded, name) VALUES (?, 1, 1, true, 'n')`, id)
				exec(t, `INSERT INTO vault.pledge (resource_id, user_id, amount, funded, frozen_at) VALUES (?, ?, 1, true, now())`, id, o)
			case "unpaid":
				exec(t, `INSERT INTO vault.resource (resource_id, required_vp, funded_vp, funded, name) VALUES (?, 1, 0, false, 'n')`, id)
				exec(t, `INSERT INTO vault.pledge (resource_id, user_id, amount, funded) VALUES (?, ?, 1, false)`, id, o)
			}
			if c.over != nil && c.over["name"] == "" {
				exec(t, `INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes) VALUES (?, 'Name', 0, 0)`, id)
			}
			if c.renamePledge {
				exec(t, `ALTER TABLE vault.pledge RENAME TO pledge_off`)
				t.Cleanup(func() { exec(t, `ALTER TABLE vault.pledge_off RENAME TO pledge`) })
			}
			var v *vault.Vault
			if c.vault != noVault {
				vpg := pgtest.CS(t, db)
				v = vault.New(cli.NewContext(cli.NewApp(), flag.NewFlagSet("v", flag.ContinueOnError), nil), &vault.Api{}, nil, nil, vpg, nil)
				if c.vault == vaultOff {
					if err := vpg.Get().Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
			r := newResourceRouterEn(t, viewAPI(t, c.over), v, &models.User{UserID: o}, pgCS, nil)
			body := getUploadPage(t, r, id)
			for _, w := range c.want {
				if !strings.Contains(body, w) {
					t.Errorf("body %q, want %s", body, w)
				}
			}
		})
	}
}
