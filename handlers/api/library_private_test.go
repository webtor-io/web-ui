package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	restapi "github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
)

// POST /library of a private torrent: rest-api decides with the user's claims.
// With a record the row is made of the .torrent web-ui reads itself; without
// one the client gets rest-api's refusal as it came and no row is written.
func TestAddLibraryPrivate(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	db := pgtest.Start(t)
	info := metainfo.Info{Name: "probe", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20), Private: &[]bool{true}[0]}
	var torrent bytes.Buffer
	if err := (&metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info)}).Write(&torrent); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name              string
		status            int
		body              string
		wantStatus        int
		wantCode, wantWhy string
		wantRow           bool
	}{
		{"a record", http.StatusOK, `{"id":"` + hash + `","private":true}`, http.StatusCreated, "", "", true},
		{"no record", http.StatusForbidden, `{"error":"x","code":"private_torrent","reason":"no_record"}`, http.StatusForbidden, "private_torrent", "no_record", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var userID uuid.UUID
			if _, err := db.QueryOne(pg.Scan(&userID), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
				t.Fatal(err)
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/resource/" + hash:
					w.WriteHeader(c.status)
					_, _ = w.Write([]byte(c.body))
				case "/resource/" + hash + ".torrent":
					_, _ = w.Write(torrent.Bytes())
				default:
					t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
				}
			}))
			t.Cleanup(upstream.Close)
			h := newRestAPIHandler(t, upstream)
			h.pg = pgtest.CS(t, db)

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(func(c *gin.Context) {
				ctx := context.WithValue(c.Request.Context(), auth.UserContext{}, &models.User{UserID: userID})
				ctx = context.WithValue(ctx, restapi.ClaimsContext{}, &restapi.Claims{Uid: "76543210fedcba9876543210fedcba9876543210"})
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			})
			r.POST("/library", h.addLibrary)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/library", strings.NewReader(`{"resource_id":"`+hash+`"}`)))

			if w.Code != c.wantStatus {
				t.Fatalf("status %d, want %d: %s", w.Code, c.wantStatus, w.Body.String())
			}
			if c.wantCode != "" {
				var res struct {
					Error struct {
						Code   string `json:"code"`
						Reason string `json:"reason"`
					} `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Error.Code != c.wantCode || res.Error.Reason != c.wantWhy {
					t.Errorf("body %s, want code %s reason %s", w.Body.String(), c.wantCode, c.wantWhy)
				}
			}
			l, err := models.GetLibraryByResourceID(context.Background(), db, userID, hash)
			if err != nil {
				t.Fatal(err)
			}
			if (l != nil) != c.wantRow {
				t.Errorf("row %+v, want one: %v", l, c.wantRow)
			}
			if l != nil && (l.Name != "probe" || l.Torrent == nil || l.Torrent.TorrentSizeBytes != int64(torrent.Len())) {
				t.Errorf("row %+v torrent %+v, want probe of %d bytes", l, l.Torrent, torrent.Len())
			}
		})
	}
}
