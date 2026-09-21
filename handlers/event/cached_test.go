package event

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeIndex struct {
	marks   []string
	unmarks []string
	err     error
}

func (f *fakeIndex) MarkFromSeeder(_ context.Context, id string, idx int) error {
	f.marks = append(f.marks, "seeder:"+id+":"+itoa(idx))
	return f.err
}

func (f *fakeIndex) UnmarkFromSeeder(_ context.Context, id string, idx *int) error {
	s := "seeder:" + id + ":*"
	if idx != nil {
		s = "seeder:" + id + ":" + itoa(*idx)
	}
	f.unmarks = append(f.unmarks, s)
	return f.err
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for ; i > 0; i /= 10 {
		s = string(rune('0'+i%10)) + s
	}
	return s
}

const h40 = "08ada5a7a6183aae1e09d831df6748d566095a10"

func TestResourceCached(t *testing.T) {
	w := "seeder"
	for _, tc := range []struct {
		name, msg string
		want      []string
	}{
		{"file", `{"resource_id":"` + h40 + `","file_idx":3}`, []string{w + ":" + h40 + ":3"}},
		// use_zero matters: index 0 is the single file of most torrents.
		{"file zero", `{"resource_id":"` + h40 + `","file_idx":0}`, []string{w + ":" + h40 + ":0"}},
		{"no index", `{"resource_id":"` + h40 + `"}`, nil},
		{"negative", `{"resource_id":"` + h40 + `","file_idx":-1}`, nil},
		{"not a hash", `{"resource_id":"../../etc","file_idx":1}`, nil},
		{"uppercase hash", `{"resource_id":"08ADA5A7A6183AAE1E09D831DF6748D566095A10","file_idx":1}`, nil},
		{"garbage", `{`, nil},
	} {
		f := &fakeIndex{}
		if err := resourceCached(f, []byte(tc.msg)); err != nil {
			t.Errorf("%s: err %v", tc.name, err)
		}
		if !equal(f.marks, tc.want) {
			t.Errorf("%s: marks %v, want %v", tc.name, f.marks, tc.want)
		}
	}
}

func TestResourceUncached(t *testing.T) {
	w := "seeder"
	for _, tc := range []struct {
		name, msg string
		want      []string
	}{
		{"file", `{"resource_id":"` + h40 + `","file_idx":0}`, []string{w + ":" + h40 + ":0"}},
		{"whole torrent", `{"resource_id":"` + h40 + `"}`, []string{w + ":" + h40 + ":*"}},
		{"empty id must not wipe", `{"resource_id":""}`, nil},
		{"garbage", `nope`, nil},
	} {
		f := &fakeIndex{}
		if err := resourceUncached(f, []byte(tc.msg)); err != nil {
			t.Errorf("%s: err %v", tc.name, err)
		}
		if !equal(f.unmarks, tc.want) {
			t.Errorf("%s: unmarks %v, want %v", tc.name, f.unmarks, tc.want)
		}
	}
}

// A database error must come back, so the message is NAKed and redelivered; a
// malformed message must not (covered above: nil error, nothing applied).
func TestResourceCacheEventErrorIsReturned(t *testing.T) {
	f := &fakeIndex{err: errors.New("db down")}
	if err := resourceCached(f, []byte(`{"resource_id":"`+h40+`","file_idx":1}`)); err == nil {
		t.Error("cached: want error")
	}
	if err := resourceUncached(f, []byte(`{"resource_id":"`+h40+`"}`)); err == nil {
		t.Error("uncached: want error")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The consumer can appear after the pod (first rollout of the chart): the
// subscription has to come up on its own, not at the next restart.
func TestRetryUntil(t *testing.T) {
	done := make(chan struct{})
	var attempts []int
	retryUntil(done, time.Millisecond, func(a int) bool {
		attempts = append(attempts, a)
		return a == 2
	})
	if len(attempts) != 3 || attempts[2] != 2 {
		t.Errorf("attempts %v, want [0 1 2]", attempts)
	}

	// Shutdown ends it even if it never succeeds.
	n := 0
	stopped := make(chan struct{})
	go func() {
		retryUntil(done, time.Hour, func(int) bool { n++; return false })
		close(stopped)
	}()
	close(done)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("did not stop on done")
	}
}
