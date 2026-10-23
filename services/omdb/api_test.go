package omdb

import (
	"context"
	"flag"
	"net/http"
	"strings"
	"testing"

	"github.com/urfave/cli"
)

// As in services/tmdb: a transport error quotes the URL, key included.
func TestTransportErrorHidesTheKey(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--omdb-api-key=SECRETKEY"}); err != nil {
		t.Fatal(err)
	}
	api := New(cli.NewContext(nil, fs, nil), http.DefaultClient)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, call := range map[string]func() error{
		"search": func() error { _, err := api.SearchByTitleAndYear(ctx, "Movie A", nil, OmdbTypeMovie); return err },
		"by id":  func() error { _, err := api.GetByIMDBID(ctx, "tt1"); return err },
	} {
		err := call()
		if err == nil || strings.Contains(err.Error(), "SECRETKEY") || !strings.Contains(err.Error(), "apikey=<redacted>") {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}
