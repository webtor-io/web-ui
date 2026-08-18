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

// Bare-keyword additions from skip-list mining 2026-08-17 (fuck*/cock/slut*/
// milf/tits/boobs + CJK cluster + Lethal Hardcore): each measured 0 FP over
// two weeks of ai_enrich.query. Negative rows guard the word-boundary
// behaviour on legitimate titles that contain the tokens as substrings.
func TestAdultBareKeywords(t *testing.T) {
	cases := []struct {
		name  string
		adult bool
	}{
		{"5 chechens are russian fucked a hot street babe", true},
		{"angela august worships ten inches of cock!", true},
		{"behind the scenes slutty bbws 2", true},
		{"aries adore depressed milf needs bbc for motivation", true},
		{"25 sexiest boobs ever cd1", true},
		{"anissa kate are my tits distracting you", true},
		{"LethalHardcore.26.08.01.Some.Scene.1080p", true},
		{"某某巨乳女神4K合集", true},
		{"素人自慰配信 2026", true},
		{"极品性爱视频合集", true},
		{"巨大肉棒中出特辑", true},
		{"ASMR 耳舐め 2026", true},
		// Substring guards: cock/tit/boob inside ordinary words must not fire.
		{"Cocktail.1988.1080p.BluRay", false},
		{"Peacock.S01E01.720p", false},
		{"Titanic.1997.2160p", false},
		{"Booba.S02.Cartoon.WEBRip", false},
		{"Milford.Graves.Full.Mantis.2018", false},
		// Standalone-token guards (review 2026-08-18): mainstream releases
		// where the bare word IS the token — the reason fuck*/cock left the
		// single-hit tier and slut* narrowed to sluts?/slutty.
		{"SVT.Slutspel.2026.S01E01.1080p.WEB", false},   // Swedish "playoffs"
		{"Slutet.2020.SWEDiSH.1080p.WEB", false},        // Swedish "the end"
		{"Fucking.Amal.1998.1080p.BluRay", false},       // Show Me Love
		{"Zero.Fucks.Given.2021.1080p.WEB", false},      // Rien à foutre
		{"Tristram.Shandy.A.Cock.and.Bull.Story.2005.720p", false},
		{"Cock.2022.Stage.Play.1080p", false},
	}
	for _, c := range cases {
		ti, err := Parse(&TorrentInfo{}, c.name)
		if err != nil {
			t.Fatalf("%q: %v", c.name, err)
		}
		if ti.Adult != c.adult {
			t.Errorf("%q: Adult = %v, want %v", c.name, ti.Adult, c.adult)
		}
	}
}
