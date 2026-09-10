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

// Weekly skiplist mining 2026-09-07. Every positive row below is a shape
// that leaked into ai_enrich.query for at least a week with 0 false
// positives in window; the negative rows are the mainstream titles the
// anchoring was designed around.
func TestSkiplist20260907Course(t *testing.T) {
	cases := []struct {
		name   string
		course bool
	}{
		// The Teaching Company / The Great Courses — anchored forms only.
		{"TTC - A History of Hitler's Empire (2nd Ed), Medbay", true},
		{"TTC-Video-The-Theory-of-Everything", true},
		{"TGC_9373_Lect30_Building_Vocabulary", true},
		{"The Great Courses Plus - The Great Tours Civil War", true},
		{"The Great Courses - Understanding Calculus", true},
		// Bare "TTC" as a word (no dash form) must stay clean.
		{"TTC 2019 Documentary", false},
		{"TTC.2019.1080p.WEB", false},
	}
	for _, c := range cases {
		ti, err := Parse(&TorrentInfo{}, c.name)
		if err != nil {
			t.Fatalf("%q: %v", c.name, err)
		}
		if ti.Course != c.course {
			t.Errorf("%q: Course = %v, want %v", c.name, ti.Course, c.course)
		}
	}
}

func TestSkiplist20260907Sport(t *testing.T) {
	cases := []struct {
		name  string
		sport bool
	}{
		// Football fixtures "<Team> vs|at <Team> DD.MM.YYYY".
		{"Real Betis vs Real Madrid 04.09.2026.mkv", true},
		{"Internazionale vs Napoli 05.09.2026", true},
		{"Nottingham Forest at Man City 30.08.2026.mkv", true},
		// Dotted / hyphenated separators in multi-word competition names.
		{"Premier.League.2026.Arsenal.v.Chelsea", true},
		{"Formula.1.2026.Round.01.Australian.GP", true},
		{"Formula-One-2026-Bahrain", true},
		{"Euro.2028.Qualifiers", true},
		{"Champions_League_2026_Final", true},
		// "vs" without the dotted fixture date is a film, not a match.
		{"Kramer vs Kramer 1979", false},
		{"Alien vs Predator 2004", false},
		{"Freddy.vs.Jason.2003.1080p", false},
	}
	for _, c := range cases {
		ti, err := Parse(&TorrentInfo{}, c.name)
		if err != nil {
			t.Fatalf("%q: %v", c.name, err)
		}
		if ti.Sport != c.sport {
			t.Errorf("%q: Sport = %v, want %v", c.name, ti.Sport, c.sport)
		}
	}
}

func TestSkiplist20260907AdultStudios(t *testing.T) {
	cases := []struct {
		name   string
		adult  bool
		studio string
	}{
		{"brazzersexxtra.26.09.01.jane.doe.some.scene", true, "brazzersexxtra"},
		{"BrazzersExxtra.26.09.01.Jane.Doe.1080p", true, "BrazzersExxtra"},
		// Existing bare form keeps matching after the optional suffix.
		{"Brazzers.26.09.01.Jane.Doe", true, "Brazzers"},
		{"myfriendshotmom.jane.doe", true, "myfriendshotmom"},
		{"MyFriendsHotMom.26.08.30.Jane.Doe.XXX.1080p", true, "MyFriendsHotMom"},
		{"Hidden-Zone.Locker.Room.HZ1234", true, "Hidden-Zone"},
		{"hidden zone 2026 collection", true, "hidden zone"},
		{"sxyprn some clip 2026", true, "sxyprn"},
		{"nsxyprn some clip 2026", true, "nsxyprn"},
		{"sorefordays.26.09.01.scene", true, "sorefordays"},
		{"nyap2p.com collection", true, "nyap2p"},
		// Substring guards.
		{"The.Hidden.Fortress.1958.Criterion", false, ""},
		{"Twilight.Zone.S01E01.1959", false, ""},
	}
	for _, c := range cases {
		ti, err := Parse(&TorrentInfo{}, c.name)
		if err != nil {
			t.Fatalf("%q: %v", c.name, err)
		}
		if ti.Adult != c.adult {
			t.Errorf("%q: Adult = %v, want %v", c.name, ti.Adult, c.adult)
		}
		if ti.Studio != c.studio {
			t.Errorf("%q: Studio = %q, want %q", c.name, ti.Studio, c.studio)
		}
	}
}

func TestSkiplist20260907AdultCJK(t *testing.T) {
	cases := []struct {
		name  string
		adult bool
	}{
		{"某某少妇4K合集", true},
		{"少婦の秘密 2026", true},
		{"极品做爱视频", true},
		{"做愛實錄 2026", true},
		{"爆乳女神合集", true},
		{"美乳人妻自拍", true},
		{"口交特辑", true},
		{"人妻 2026 collection", true},
		// 偷情 is the CN release title of "Closer" (2004) — deliberately
		// NOT a marker.
		{"Closer.2004.偷情.1080p.BluRay", false},
		{"偷情 Closer 2004", false},
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

// CN "visit our site for more" banners in fullwidth brackets. Without a
// Website match the leading form leaks into Title verbatim and the AI
// resolver is asked about "【更多高清电影请访问 …】Movie Name".
func TestSkiplist20260907SiteBanner(t *testing.T) {
	cases := []struct {
		name        string
		wantTitle   string
		wantWebsite string
	}{
		{"【更多高清电影请访问 www.example.com】Movie.Name.2020.1080p.mkv", "Movie Name", "更多高清电影请访问 www.example.com"},
		{"【更多资源訪問 example.com】Movie.Name.2020.1080p.mkv", "Movie Name", "更多资源訪問 example.com"},
		{"【访问 example.com 获取更多】Movie.Name.2020", "Movie Name", "访问 example.com 获取更多"},
		{"Movie.Name.2020.1080p【更多资源请访问 example.com】.mkv", "Movie Name", "更多资源请访问 example.com"},
		// Fullwidth brackets without the site-banner keywords are left
		// alone — fansub group tags are a different (existing) problem.
		{"【字幕组】Movie.Name.2020.1080p", "【字幕组】Movie Name", ""},
	}
	for _, c := range cases {
		ti, err := Parse(&TorrentInfo{}, c.name)
		if err != nil {
			t.Fatalf("%q: %v", c.name, err)
		}
		if ti.Title != c.wantTitle {
			t.Errorf("%q: Title = %q, want %q", c.name, ti.Title, c.wantTitle)
		}
		if ti.Website != c.wantWebsite {
			t.Errorf("%q: Website = %q, want %q", c.name, ti.Website, c.wantWebsite)
		}
	}
}
