package parsetorrentname

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

var updateGoldenFiles = flag.Bool("update", false, "update golden files in testdata/")

// goldenFixture is the on-disk shape of each testdata/golden_file_*.json
// fixture: the original filename to feed Parse, plus the expected
// TorrentInfo result. Storing both in the same file makes each fixture
// self-describing — no need to cross-reference an index-keyed slice of
// inputs to see what produced a given golden output.
//
// To add a new test case: drop a new file into testdata/ with the
// `input` field filled and `want` empty, then `go test -update` —
// the runner re-parses each fixture's input and writes the result
// into want.
type goldenFixture struct {
	Input string      `json:"input"`
	Want  TorrentInfo `json:"want"`
}

func TestParser(t *testing.T) {
	matches, err := filepath.Glob("testdata/golden_file_*.json")
	if err != nil {
		t.Fatalf("glob testdata: %v", err)
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		t.Fatal("no golden fixtures found in testdata/")
	}

	for _, path := range matches {
		path := path
		name := filepath.Base(path)
		name = name[:len(name)-len(filepath.Ext(name))]
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			var fx goldenFixture
			if err := json.Unmarshal(data, &fx); err != nil {
				t.Fatalf("unmarshal %s: %v", path, err)
			}
			if fx.Input == "" {
				t.Fatalf("%s: missing or empty `input` field", path)
			}

			got, err := Parse(&TorrentInfo{}, fx.Input)
			if err != nil {
				t.Fatalf("Parse(%q): %v", fx.Input, err)
			}

			if *updateGoldenFiles {
				fx.Want = *got
				buf, err := json.MarshalIndent(&fx, "", "  ")
				if err != nil {
					t.Fatalf("marshal %s: %v", path, err)
				}
				if err := os.WriteFile(path, buf, 0644); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
				return
			}

			if !reflect.DeepEqual(*got, fx.Want) {
				t.Fatalf("%s\ninput: %q\nwant:\n  %+v\ngot:\n  %+v",
					path, fx.Input, fx.Want, *got)
			}
		})
	}
}
