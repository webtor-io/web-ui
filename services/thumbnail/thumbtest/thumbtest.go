// Package thumbtest is an in-memory S3 for tests of thumbnails and their renders.
package thumbtest

import (
	"encoding/xml"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/thumbnail"
)

// Store keeps objects by "<bucket>/<key>" and counts the requests it got by method.
type Store struct {
	mu         sync.Mutex
	objs       map[string][]byte
	hits       map[string]int
	fail       string // requests with this method are answered 500
	holdMethod string // requests with this method are answered hold late
	hold       time.Duration
}

// Start serves a Store over HTTP and returns an S3 client pointed at it (path-style:
// /<bucket>/<key>). It answers PUT, GET and DELETE of an object and ListObjectsV2 of
// a bucket by prefix; a GET of a key it does not have is S3's 404 NoSuchKey.
func Start(t *testing.T) (*cs.S3Client, *Store) {
	t.Helper()
	s := &Store{objs: map[string][]byte{}, hits: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	fs := flag.NewFlagSet("thumbtest", flag.ContinueOnError)
	for _, f := range cs.RegisterS3ClientFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--aws-access-key-id=test", "--aws-secret-access-key=test",
		"--aws-endpoint=" + srv.URL, "--aws-region=us-east-1", "--aws-no-ssl"}); err != nil {
		t.Fatal(err)
	}
	return cs.NewS3Client(cli.NewContext(cli.NewApp(), fs, nil), srv.Client()), s
}

type listResult struct {
	XMLName     xml.Name `xml:"ListBucketResult"`
	Name        string
	Prefix      string
	KeyCount    int
	IsTruncated bool
	Contents    []struct{ Key string }
}

func (s *Store) serve(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/")
	var body []byte
	if r.Method == http.MethodPut {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		body = b
	}
	s.mu.Lock()
	s.hits[r.Method]++
	if r.Method == s.fail {
		s.mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	status, out := http.StatusOK, []byte(nil)
	switch {
	case r.Method == http.MethodPut:
		s.objs[key] = body
	case r.Method == http.MethodDelete:
		delete(s.objs, key)
		status = http.StatusNoContent
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		res := listResult{Name: key, Prefix: r.URL.Query().Get("prefix")}
		for _, k := range s.keys(key) {
			if strings.HasPrefix(k, res.Prefix) {
				res.Contents = append(res.Contents, struct{ Key string }{k})
			}
		}
		res.KeyCount = len(res.Contents)
		out, _ = xml.Marshal(res)
		w.Header().Set("Content-Type", "application/xml")
	case r.Method == http.MethodGet:
		b, ok := s.objs[key]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			status, out = http.StatusNotFound, []byte(`<Error><Code>NoSuchKey</Code></Error>`)
		} else {
			out = b
		}
	default:
		status = http.StatusMethodNotAllowed
	}
	var hold time.Duration
	if r.Method == s.holdMethod {
		hold = s.hold
	}
	s.mu.Unlock()
	time.Sleep(hold)
	w.WriteHeader(status)
	_, _ = w.Write(out)
}

// Put stores an object as if it had been uploaded.
func (s *Store) Put(bucket, key string, b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objs[bucket+"/"+key] = b
}

// Keys lists the keys of a bucket, sorted.
func (s *Store) Keys(bucket string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keys(bucket)
}

func (s *Store) keys(bucket string) []string {
	var keys []string
	for k := range s.objs {
		if rest, ok := strings.CutPrefix(k, bucket+"/"); ok {
			keys = append(keys, rest)
		}
	}
	sort.Strings(keys)
	return keys
}

// Hits is how many requests with this method the store has answered.
func (s *Store) Hits(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[method]
}

// Fail answers every request with this method 500, until Fail("").
func (s *Store) Fail(method string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = method
}

// Hold applies a request with this method at once and answers it d later,
// until Hold("", 0).
func (s *Store) Hold(method string, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.holdMethod, s.hold = method, d
}

// Service is a thumbnail service on s3 and pg, its bucket "thumbs" and its
// renders' "renders". flags go after these, and the last value of a flag
// wins: "--aws-poster-cache-bucket=" is a service with no render cache.
func Service(t *testing.T, s3 *cs.S3Client, pg *cs.PG, a *api.Api, flags ...string) *thumbnail.Service {
	t.Helper()
	fs := flag.NewFlagSet("thumbtest", flag.ContinueOnError)
	for _, f := range thumbnail.RegisterFlags([]cli.Flag{cli.StringFlag{Name: thumbnail.PosterCacheBucketFlag}}) {
		f.Apply(fs)
	}
	if err := fs.Parse(append([]string{"--aws-thumbnail-bucket=thumbs", "--" + thumbnail.PosterCacheBucketFlag + "=renders"}, flags...)); err != nil {
		t.Fatal(err)
	}
	th := thumbnail.New(cli.NewContext(cli.NewApp(), fs, nil), s3, pg, a, nil)
	if th == nil {
		t.Fatal("thumbnail service is not configured")
	}
	return th
}
