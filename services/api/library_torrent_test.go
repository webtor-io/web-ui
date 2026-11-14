package api

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/pkg/errors"
)

// rest-api decides who may add a torrent to a library: the resource is asked
// for with the user's claims. The .torrent the row is made of is then read as
// web-ui itself, since rest-api serves a private torrent's .torrent to no user.
func TestLibraryTorrent(t *testing.T) {
	const uid = "76543210fedcba9876543210fedcba9876543210"
	info := metainfo.Info{Name: "probe", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20), Private: &[]bool{true}[0]}
	var torrent bytes.Buffer
	if err := (&metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info)}).Write(&torrent); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		status   int
		body     string
		code     string
		wantInfo bool
		torrent  bool // the .torrent is asked for
	}{
		{"private with a record", http.StatusOK, `{"id":"` + cacheTestHash + `","private":true}`, "", true, true},
		{"no record", http.StatusForbidden, `{"error":"x","code":"private_torrent","reason":"no_record"}`, CodePrivateTorrent, false, false},
		{"not stored", http.StatusNotFound, `{"error":"not found"}`, "", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			asked := false
			s := restAPIStub(t, func(w http.ResponseWriter, r *http.Request) {
				cl := claimsOf(t, r)
				switch r.URL.Path {
				case "/resource/" + cacheTestHash:
					if cl["uid"] != uid || cl["internal"] != nil {
						t.Errorf("resource token: uid %v internal %v, want the user's", cl["uid"], cl["internal"])
					}
					w.WriteHeader(c.status)
					_, _ = w.Write([]byte(c.body))
				case "/resource/" + cacheTestHash + ".torrent":
					asked = true
					if cl["internal"] != true || cl["uid"] != nil {
						t.Errorf(".torrent token: uid %v internal %v, want web-ui's own", cl["uid"], cl["internal"])
					}
					_, _ = w.Write(torrent.Bytes())
				default:
					t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
				}
			})
			got, size, err := s.LibraryTorrent(context.Background(), &Claims{Uid: uid}, cacheTestHash)
			var pe *PrivateError
			if c.code != "" {
				if !errors.As(err, &pe) || pe.Code != c.code || pe.Reason != ReasonNoRecord {
					t.Errorf("err = %#v, want *PrivateError %s", err, c.code)
				}
			} else if err != nil {
				t.Errorf("err = %v", err)
			}
			if c.wantInfo {
				if got == nil || got.Name != "probe" || size != int64(torrent.Len()) {
					t.Errorf("info %+v size %d, want probe of %d bytes", got, size, torrent.Len())
				}
			} else if got != nil || size != 0 {
				t.Errorf("info %+v size %d, want nothing", got, size)
			}
			if asked != c.torrent {
				t.Errorf(".torrent asked for: %v, want %v", asked, c.torrent)
			}
		})
	}
}
