package api

import (
	"bytes"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/urfave/cli"

	restapi "github.com/webtor-io/web-ui/services/api"
)

// A private .torrent stored through the JSON API is counted once, as the
// API's (services/private_torrent); a magnet is not a torrent and is not.
func TestPostResource_CountsAPrivateUpload(t *testing.T) {
	info := metainfo.Info{Name: "probe", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20), Private: &[]bool{true}[0]}
	mi := metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info), Announce: "http://tracker.invalid/announce?passkey=0123456789abcdef0123456789abcdef"}
	var torrent bytes.Buffer
	if err := mi.Write(&torrent); err != nil {
		t.Fatal(err)
	}
	hash := mi.HashInfoBytes().HexString()

	restAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/resource" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + hash + `"}`))
	}))
	t.Cleanup(restAPI.Close)
	u, _ := url.Parse(restAPI.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range restapi.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{api: restapi.New(cli.NewContext(cli.NewApp(), fs, nil), restAPI.Client())}
	gin.SetMode(gin.TestMode)

	count := func(body []byte) int {
		hook := test.NewGlobal()
		defer hook.Reset()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/resource", bytes.NewReader(body))
		h.postResource(c)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		n := 0
		for _, e := range hook.AllEntries() {
			if e.Data["event"] == "private_torrent_upload" {
				if e.Data["source"] != "api" || e.Data["user"] != "anon" || e.Data["infohash"] != hash {
					t.Errorf("line %v", e.Data)
				}
				n++
			}
		}
		return n
	}
	if n := count(torrent.Bytes()); n != 1 {
		t.Errorf("a private torrent: %d lines, want 1", n)
	}
	if n := count([]byte("magnet:?xt=urn:btih:" + hash)); n != 0 {
		t.Errorf("a magnet: %d lines, want 0", n)
	}
}
