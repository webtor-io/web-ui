package uploads

import (
	"flag"
	"strings"
	"testing"

	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/api"
)

func TestPredicates(t *testing.T) {
	const (
		kind0 = "9c5db5752b7c0123456789abcdef0123456789ab"
		kind1 = "9c5db5752b7c1123456789abcdef0123456789ab"
		kindF = "9c5db5752b7cf123456789abcdef0123456789ab"
	)
	for _, c := range []struct {
		id          string
		home, owned bool
	}{
		{kind0, true, true},
		{kind1, true, false},
		{kindF, true, false},
		{strings.ToUpper(kind0), false, false},
		{strings.ToUpper(kind1), false, false},
		{strings.ToUpper(kindF), false, false},
		{"9c5db5752b7cA123456789abcdef0123456789ab", false, false},
		{"9c5db5752b7c/../x", false, false},
		{"9c5db5752b7c0123456789abcdef0123456789a", false, false},
		{"0123456789abcdef0123456789abcdef01234567", false, false},
	} {
		if home, owned := HomeHosted(c.id), Owned(c.id); home != c.home || owned != c.owned {
			t.Errorf("%q: HomeHosted %v, Owned %v; want %v, %v", c.id, home, owned, c.home, c.owned)
		}
	}
}

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id, err := NewID()
		if err != nil {
			t.Fatal(err)
		}
		if len(id) != 40 || strings.Trim(id, "0123456789abcdef") != "" {
			t.Fatalf("%q is not 40 lower-case hex", id)
		}
		if !strings.HasPrefix(id, "9c5db5752b7c") || id[12] != '0' || !Owned(id) {
			t.Fatalf("%q is not an upload ID of kind 0", id)
		}
		if seen[id] {
			t.Fatalf("%q twice", id)
		}
		seen[id] = true
	}
}

func cliContext(t *testing.T, args ...string) *cli.Context {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, fl := range RegisterFlags(api.RegisterFlags(nil)) {
		fl.Apply(fs)
	}
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return cli.NewContext(cli.NewApp(), fs, nil)
}

// Every flag is given, empty ones too: the environment must not decide.
func TestEnabled(t *testing.T) {
	hook := logtest.NewGlobal()
	for _, c := range []struct {
		name string
		args []string
		on   bool
		line string
	}{
		{"configured", []string{"--upload-url=https://up.example.invalid", "--webtor-secret=test", "--webtor-key=k", "--rapidapi-host=", "--rapidapi-key="},
			true, "uploads: on"},
		{"no address", []string{"--upload-url=", "--webtor-secret=test", "--webtor-key=k", "--rapidapi-host=", "--rapidapi-key="},
			false, "uploads: off, UPLOAD_URL is not set"},
		{"no secret", []string{"--upload-url=https://up.example.invalid", "--webtor-secret=", "--webtor-key=k", "--rapidapi-host=", "--rapidapi-key="},
			false, "uploads: off, WEBTOR_API_SECRET is not set"},
		{"RapidAPI", []string{"--upload-url=https://up.example.invalid", "--webtor-secret=test", "--webtor-key=k", "--rapidapi-host=rapid.example.invalid", "--rapidapi-key=r"},
			false, "uploads: off, private resources are not available through RapidAPI"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := cliContext(t, c.args...)
			hook.Reset()
			if got := Enabled(ctx); got != c.on {
				t.Errorf("Enabled %v, want %v", got, c.on)
			}
			if s := New(ctx, nil, nil); (s != nil) != c.on {
				t.Errorf("New %v, want a service %v", s, c.on)
			}
			if len(hook.Entries) != 1 || hook.LastEntry().Message != c.line {
				t.Errorf("log %v, want one line %q", hook.AllEntries(), c.line)
			}
		})
	}
}
