package private_torrent

import (
	"bytes"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"

	"github.com/webtor-io/web-ui/services/auth"
)

const passkey = "0123456789abcdef0123456789abcdef"

// makeTorrent is a .torrent with the given private flag (nil: none) and
// trackers, the first as "announce", all of them in "announce-list".
func makeTorrent(t *testing.T, private *bool, trackers ...string) []byte {
	t.Helper()
	info := metainfo.Info{Name: "probe", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20), Private: private}
	mi := metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info)}
	if len(trackers) > 0 {
		mi.Announce = trackers[0]
		mi.AnnounceList = [][]string{trackers}
	}
	var b bytes.Buffer
	if err := mi.Write(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func ptr(b bool) *bool { return &b }

// One line per private torrent that comes in, for counting them in Loki: who
// (the account's log pseudonym), which torrent, which trackers -- scheme,
// host and port only, never the path or the query a passkey rides in -- and
// through which door.
func TestLogUpload_PrivateTorrent(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	b := makeTorrent(t, ptr(true),
		"http://tracker.invalid:8080/announce.php?passkey="+passkey,
		"https://tracker.invalid/"+passkey+"/announce",
		"udp://Tracker.Invalid:6969/announce",
		"http://tracker.invalid:8080/announce.php?pk="+passkey,
		"http://user:"+passkey+"@other.invalid/announce",
		"%zz not a url",
		"",
	)
	mi, err := metainfo.Load(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewV4()

	LogUpload(b, &auth.User{ID: id}, "site")

	entries := hook.AllEntries()
	if len(entries) != 1 {
		t.Fatalf("%d lines, want 1", len(entries))
	}
	e := entries[0]
	if e.Level != log.InfoLevel {
		t.Errorf("level %v, want info", e.Level)
	}
	for k, want := range map[string]string{
		"event":    "private_torrent_upload",
		"user":     auth.LogHash(id),
		"infohash": mi.HashInfoBytes().HexString(),
		"trackers": "http://tracker.invalid:8080,https://tracker.invalid,udp://tracker.invalid:6969,http://other.invalid",
		"source":   "site",
	} {
		if got := e.Data[k]; got != want {
			t.Errorf("%s = %v, want %s", k, got, want)
		}
	}
	if line, _ := e.String(); strings.Contains(line, passkey) || strings.Contains(line, "announce") {
		t.Errorf("the line keeps a tracker's path or query: %s", line)
	}
}

func TestLogUpload_Anonymous(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	b := makeTorrent(t, ptr(true), "http://tracker.invalid/announce")
	LogUpload(b, nil, "api")
	LogUpload(b, &auth.User{}, "embed")
	entries := hook.AllEntries()
	if len(entries) != 2 {
		t.Fatalf("%d lines, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Data["user"] != "anon" {
			t.Errorf("user = %v, want anon", e.Data["user"])
		}
	}
}

// Only a private torrent is counted; anything else, a magnet or junk
// included, is not a line. A measurement: nothing here may change what the
// upload does, so there is no error to return.
func TestLogUpload_NotPrivate(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	for name, b := range map[string][]byte{
		"public":      makeTorrent(t, nil, "http://tracker.invalid/announce"),
		"private=0":   makeTorrent(t, ptr(false), "http://tracker.invalid/announce"),
		"a magnet":    []byte("magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10"),
		"junk":        []byte("d4:info"),
		"nothing":     nil,
		"info is not": []byte("d4:infoi1ee"),
	} {
		LogUpload(b, nil, "api")
		if n := len(hook.AllEntries()); n != 0 {
			t.Fatalf("%s: %d lines, want none", name, n)
		}
	}
}
