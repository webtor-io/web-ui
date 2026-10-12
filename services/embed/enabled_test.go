package embed

import (
	"flag"
	"testing"

	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/services/common"
)

func TestEnabled(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{nil, true}, // USE_EMBED defaults to true
		{[]string{"--use-embed=false"}, false},
		{[]string{"--disable-embed"}, false},
		{[]string{"--use-embed=true", "--disable-embed"}, false},
	} {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		for _, f := range RegisterFlags(nil) {
			f.Apply(fs)
		}
		cli.BoolFlag{Name: common.DisableEmbedFlag}.Apply(fs)
		if err := fs.Parse(c.args); err != nil {
			t.Fatal(err)
		}
		if got := Enabled(cli.NewContext(nil, fs, nil)); got != c.want {
			t.Errorf("%v: Enabled %v, want %v", c.args, got, c.want)
		}
	}
}
