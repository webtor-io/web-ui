package tmdb

import (
	"context"
	"flag"
	"net/http"
	"strings"
	"testing"

	"github.com/urfave/cli"
)

// A failed request's error quotes the URL, and the key rides in its query;
// every caller logs that error. It must reach them without the key.
func TestTransportErrorHidesTheKey(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--tmdb-api-key=SECRETKEY"}); err != nil {
		t.Fatal(err)
	}
	api := New(cli.NewContext(nil, fs, nil), http.DefaultClient)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := api.GetDetails(ctx, 1, TmdbTypeMovie)
	if err == nil {
		t.Fatal("a canceled request did not fail")
	}
	if strings.Contains(err.Error(), "SECRETKEY") || !strings.Contains(err.Error(), "api_key=<redacted>") {
		t.Errorf("error = %q", err)
	}
}
