package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"

	restapi "github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/libapi"
)

// countingTransport counts what would reach rest-api and answers nothing.
type countingTransport struct{ n int }

func (t *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.n++
	return nil, errors.New("rest-api is not reached in this test")
}

// Through RapidAPI one uid serves every user of the instance (decision 42):
// the JSON API refuses a private .torrent with 422 private_unavailable in its
// envelope, and rest-api is not asked.
func TestJSONAPIRapidAPIModePrivate(t *testing.T) {
	info := metainfo.Info{Name: "probe", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20), Private: &[]bool{true}[0]}
	mi := metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info), Announce: "http://tracker.invalid/announce"}
	var torrent bytes.Buffer
	if err := mi.Write(&torrent); err != nil {
		t.Fatal(err)
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range restapi.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--rapidapi-host=gateway.invalid", "--rapidapi-key=0000"}); err != nil {
		t.Fatal(err)
	}
	rt := &countingTransport{}
	h := &Handler{api: restapi.New(cli.NewContext(cli.NewApp(), fs, nil), &http.Client{Transport: rt})}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/resource", h.postResource)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/resource", bytes.NewReader(torrent.Bytes())))

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", w.Code, w.Body.String())
	}
	var res libapi.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Error.Code != restapi.CodePrivateUnavailable {
		t.Errorf("body %s, want code %s", w.Body.String(), restapi.CodePrivateUnavailable)
	}
	if rt.n != 0 {
		t.Errorf("rest-api got %d requests, want none", rt.n)
	}
}
