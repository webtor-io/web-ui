package api

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// drainStats reads the seeder's stats stream until it closes.
func drainStats(t *testing.T, ch <-chan EventData, d time.Duration) []EventData {
	t.Helper()
	var got []EventData
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("stats stream did not close within %v (got %d events)", d, len(got))
			return got
		}
	}
}

// bigFrame is a statupdate line of at least size bytes: the first frame of a
// stream lists every piece of the torrent (~405 KB at 8k pieces, ~3.2 MB at
// 64k).
func bigFrame(size int) string {
	var b strings.Builder
	b.WriteString(`{"total":1073741824,"completed":1024,"peers":3,"pieces":[`)
	for i := 0; b.Len() < size; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"position":%d,"complete":%v,"priority":0}`, i, i%2 == 0)
	}
	b.WriteString(`]}`)
	return b.String()
}

// A torrent with tens of thousands of pieces opens its stream with a frame
// of megabytes. The line cap used to be 1 MB: past it the scanner stopped,
// the stream closed on its first frame, and such a torrent never had a
// status. The buffer still starts at 4 KB and grows only for the frames
// that need it (see the ~300 MB note in Stats).
func TestStats_ParsesAFrameOfSeveralMegabytes(t *testing.T) {
	frame := bigFrame(4 << 20)
	if len(frame) < 4<<20 {
		t.Fatalf("the synthetic frame is %d bytes", len(frame))
	}
	srv := sseServer("event: statupdate\ndata: " + frame + "\n\n" +
		"event: statupdate\ndata: {\"total\":1073741824,\"completed\":2048,\"peers\":3}\n\n")
	defer srv.Close()
	a := &Api{cl: srv.Client()}
	ch, err := a.Stats(context.Background(), srv.URL+"/stats")
	if err != nil {
		t.Fatal(err)
	}
	got := drainStats(t, ch, 5*time.Second)
	if len(got) != 2 {
		t.Fatalf("want both frames, got %d", len(got))
	}
	if n := len(got[0].Pieces); n < 80000 || !got[0].Pieces[0].Complete || got[0].Pieces[1].Complete {
		t.Errorf("the big frame decoded %d pieces", n)
	}
	if got[1].Completed != 2048 {
		t.Errorf("the frame after it: %+v", got[1])
	}
	if statsMaxLine < 16<<20 {
		t.Errorf("line cap %d: a 64k-piece torrent's first frame is ~3.2 MB, a 256k one's ~13 MB", statsMaxLine)
	}
}

// The seeder's swarm availability (torrent-web-seeder StatReply fields 9-14):
// decoded as sent, and absent from an older seeder -- which reads as "not
// known", with nothing missing.
func TestStats_DecodesAvailability(t *testing.T) {
	srv := sseServer(
		"event: statupdate\ndata: {\"total\":10,\"completed\":1,\"peers\":12,\"seeders\":0,\"availability\":0.73,\"availability_known\":true," +
			"\"missing\":[{\"start\":3,\"end\":5},{\"start\":7,\"end\":8}],\"missing_unchanged\":false,\"wanted_missing\":2,\"reader_missing\":1}\n\n" +
			"event: statupdate\ndata: {\"total\":10,\"completed\":2,\"availability\":0.73,\"availability_known\":true,\"missing\":null,\"missing_unchanged\":true,\"wanted_missing\":2,\"reader_missing\":0}\n\n" +
			"event: statupdate\ndata: {\"total\":10,\"completed\":3,\"peers\":1}\n\n")
	defer srv.Close()
	a := &Api{cl: srv.Client()}
	ch, err := a.Stats(context.Background(), srv.URL+"/stats")
	if err != nil {
		t.Fatal(err)
	}
	got := drainStats(t, ch, 2*time.Second)
	if len(got) != 3 {
		t.Fatalf("want 3 frames, got %d", len(got))
	}
	first := got[0]
	if !first.AvailabilityKnown || first.Availability != 0.73 || first.WantedMissing != 2 || first.ReaderMissing != 1 || first.MissingUnchanged {
		t.Errorf("first frame: %+v", first)
	}
	if len(first.Missing) != 2 || first.Missing[0] != (PieceRange{Start: 3, End: 5}) || first.Missing[1] != (PieceRange{Start: 7, End: 8}) {
		t.Errorf("missing runs: %+v", first.Missing)
	}
	if !got[1].MissingUnchanged || got[1].Missing != nil {
		t.Errorf("an unchanged frame: %+v", got[1])
	}
	if old := got[2]; old.AvailabilityKnown || old.Missing != nil || old.MissingUnchanged || old.WantedMissing != 0 || old.ReaderMissing != 0 {
		t.Errorf("a frame without the fields: %+v", old)
	}
}

// holdingSSE serves body, then keeps the stream open and silent until the
// client goes -- a stats stream on an open page.
func holdingSSE(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, body)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
}

// heapAfterGC is the live heap, collected.
func heapAfterGC() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// A stream keeps a 4 KB read buffer for its whole life, whatever its first
// frame needed: the frame of a 64k-piece torrent (~3.2 MB) is put together,
// decoded and let go. A bufio.Scanner kept its grown buffer -- 4 MB a
// stream, for as long as the page stayed open -- and pre-allocating the cap
// would keep 16 MB; either reads here as megabytes a stream.
func TestStats_BigFrameIsNotKeptForTheStream(t *testing.T) {
	const streams = 8
	frame := bigFrame(3200 << 10)
	srv := holdingSSE("event: statupdate\ndata: " + frame + "\n\n")
	defer srv.Close()
	a := &Api{cl: srv.Client()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	before := heapAfterGC()
	chans := make([]chan EventData, 0, streams)
	for i := 0; i < streams; i++ {
		ch, err := a.Stats(ctx, srv.URL+"/stats")
		if err != nil {
			t.Fatal(err)
		}
		select {
		case ev := <-ch:
			if len(ev.Pieces) < 60000 {
				t.Fatalf("stream %d: the first frame decoded %d pieces", i, len(ev.Pieces))
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("stream %d: no first frame", i)
		}
		chans = append(chans, ch)
	}
	after := heapAfterGC()
	per := (int64(after) - int64(before)) / streams
	t.Logf("retained a stream after a %d KB first frame: %d KB", len(frame)>>10, per>>10)
	if per > 512<<10 {
		t.Errorf("a stream keeps %d KB after its first frame, want the read buffer's few", per>>10)
	}
	runtime.KeepAlive(chans)
}

// A frame over the cap costs that frame, not the stream: the ones after it
// are whole in everything but the pieces, which come as diffs. (Closing the
// stream there left the page on "idle" -- nothing said why.)
func TestStats_FrameOverTheCapIsSkipped(t *testing.T) {
	srv := sseServer("event: statupdate\ndata: {\"total\":10,\"completed\":1}\n\n" +
		"event: statupdate\ndata: " + bigFrame(statsMaxLine+1) + "\n\n" +
		"event: statupdate\r\ndata: {\"total\":10,\"completed\":3}\r\n\r\n")
	defer srv.Close()
	a := &Api{cl: srv.Client()}
	ch, err := a.Stats(context.Background(), srv.URL+"/stats")
	if err != nil {
		t.Fatal(err)
	}
	got := drainStats(t, ch, 20*time.Second)
	if len(got) != 2 || got[0].Completed != 1 || got[1].Completed != 3 {
		t.Fatalf("want the frames around the long one, got %d: %+v", len(got), got)
	}
}

// The line reader on its own, with a small cap: lines that fit the buffer,
// lines put together from several reads, CRLF, a line over the cap dropped
// to its end, and a last line without its newline.
func TestStatsLines(t *testing.T) {
	long := strings.Repeat("x", 50)
	over := strings.Repeat("y", 120)
	in := "a\n" + long + "\r\n" + over + "\nb\nlast"
	l := statsLines{r: bufio.NewReaderSize(strings.NewReader(in), 16), max: 100}
	var got []string
	for {
		line, err := l.next()
		if err == errStatsLineTooLong {
			got = append(got, "<over>")
			continue
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(line))
	}
	want := []string{"a", long, "<over>", "b", "last"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}
