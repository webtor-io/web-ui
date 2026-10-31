package enrich

import (
	"context"
	"flag"
	"net/http"
	"testing"

	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/tmdb"
)

// A tmdbN id whose type cannot be read must not be guessed as a movie: a
// series asked for as a movie answers 404, and that is stored as a
// permanent empty row for (id, lang).
func TestResolveLocalizeIDsFailedReadIsAnError(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range append(tmdb.RegisterFlags(nil), cs.RegisterPGFlags(nil)...) {
		f.Apply(fs)
	}
	// Nothing listens on port 1: every read fails at once.
	if err := fs.Parse([]string{
		"--tmdb-api-key", "k",
		"--postgres-host", "127.0.0.1", "--postgres-port", "1", "--postgres-user", "u",
		"--postgres-database", "d", "--postgres-max-retries", "0",
	}); err != nil {
		t.Fatal(err)
	}
	c := cli.NewContext(cli.NewApp(), fs, nil)
	csPG := cs.NewPG(c)
	t.Cleanup(csPG.Close)
	s := NewTMDB(csPG, tmdb.New(c, http.DefaultClient))

	id, typ, err := s.resolveLocalizeIDs(context.Background(), "tmdb42")
	if err == nil {
		t.Fatalf("got (%d, %q, nil), want an error", id, typ)
	}
}
