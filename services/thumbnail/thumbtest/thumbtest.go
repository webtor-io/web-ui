// Package thumbtest is an in-memory S3 for tests of thumbnails and their renders.
package thumbtest

import (
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/thumbnail"
)

// Store keeps objects by "<bucket>/<key>" and counts the requests it got by method.
type Store struct {
	mu   sync.Mutex
	objs map[string][]byte
	hits map[string]int
}

// Start serves a Store over HTTP and returns an S3 client pointed at it (path-style:
// /<bucket>/<key>). It answers PUT and GET of an object; a GET of a key it does not
// have is S3's 404 NoSuchKey.
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

func (s *Store) serve(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hits[r.Method]++
	switch r.Method {
	case http.MethodPut:
		b, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s.objs[key] = b
	case http.MethodGet:
		b, ok := s.objs[key]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
			return
		}
		_, _ = w.Write(b)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
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

// Service is a thumbnail service on s3 and pg, its bucket "thumbs".
func Service(t *testing.T, s3 *cs.S3Client, pg *cs.PG, a *api.Api) *thumbnail.Service {
	t.Helper()
	fs := flag.NewFlagSet("thumbtest", flag.ContinueOnError)
	for _, f := range thumbnail.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--aws-thumbnail-bucket=thumbs"}); err != nil {
		t.Fatal(err)
	}
	th := thumbnail.New(cli.NewContext(cli.NewApp(), fs, nil), s3, pg, a, nil)
	if th == nil {
		t.Fatal("thumbnail service is not configured")
	}
	return th
}
