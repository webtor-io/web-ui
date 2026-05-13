package parsetorrentname

import (
	"reflect"
	"strconv"
	"strings"
)

// TorrentInfo is the resulting structure returned by Parse
type TorrentInfo struct {
	Title       string `json:"title,omitempty"`
	ExtraTitle  string `json:"extra_title,omitempty"`
	Season      int    `json:"season,omitempty"`
	Episode     int    `json:"episode,omitempty"`
	Year        int    `json:"year,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	Bitrate     string `json:"bitrate,omitempty"`
	Quality     string `json:"quality,omitempty"`
	ColorDepth  string `json:"color_depth,omitempty"`
	Codec       string `json:"codec,omitempty"`
	Audio       string `json:"audio,omitempty"`
	Group       string `json:"group,omitempty"`
	Studio      string `json:"studio,omitempty"`
	Region      string `json:"region,omitempty"`
	Extended    bool   `json:"extended,omitempty"`
	Hardcoded   bool   `json:"hardcoded,omitempty"`
	Proper      bool   `json:"proper,omitempty"`
	Repack      bool   `json:"repack,omitempty"`
	Container   string `json:"container,omitempty"`
	Widescreen  bool   `json:"widescreen,omitempty"`
	Website     string `json:"website,omitempty"`
	Language    string `json:"language,omitempty"`
	Sbs         string `json:"sbs,omitempty"`
	Unrated     bool   `json:"unrated,omitempty"`
	Size        string `json:"size,omitempty"`
	Threed      bool   `json:"3d,omitempty"`
	Adult       bool   `json:"adult,omitempty"`
	Avc         bool   `json:"avc,omitempty"`
	SplitScenes bool   `json:"split_scenes,omitempty"`
	Scene       int    `json:"scene,omitempty"`
	Dubbing     string `json:"dubbing,omitempty"`
	Sample      bool   `json:"sample,omitempty"`
	RipBy       string `json:"rip_by,omitempty"`
	// Kind tags the release-segment type for anime extras: ONA (Original
	// Net Animation), OVA (Original Video Animation), OAD (Original
	// Animation DVD), NCOP (Non-Credit Opening), NCED (Non-Credit Ending).
	// Episode extractor still produces the numeric episode index alongside.
	Kind string `json:"kind,omitempty"`
	// Date is the scene-release date extracted from adult / dated-release
	// filenames. Normalized to "YYYY-MM-DD" regardless of source format:
	//   - "Blacked.18.03.21.Lana.Rhoades"     → 2018-03-21
	//   - "hegre 23 08 22 allie"              → 2023-08-22
	//   - "Studio - Title (27.02.2026) rq"    → 2026-02-27
	//   - "bex_stormy_daniels_kl040518_480p"  → 2018-05-04 (YYMMDD glued)
	// Two-digit years assume 20YY (adult-scene convention since ~2000).
	Date string `json:"date,omitempty"`
	// Extra collects any input bytes that no field parser (nor Title)
	// claimed — typically content inside `(...)` parens, language tags,
	// fansub annotations, etc. Bracket characters and pure-separator
	// runs are trimmed; remaining fragments are joined by a single
	// space so the field stays readable. Useful both as a debugging
	// signal ("what didn't we parse out?") and as a place downstream
	// consumers can look for incidental metadata like dubbing language.
	Extra string `json:"extra,omitempty"`
	// Sport is set when the filename carries a recognised league/event
	// marker (NBA, NHL, WWE, AEW, UFC, Champions League, КХЛ, etc.).
	// Downstream enrichment skips AI fallback for these — TMDB/OMDB/
	// KPU don't index sports broadcasts and Claude has nothing useful
	// to add. Detected via TransientFieldParser so the matched keyword
	// stays visible in Title/Extra rather than being silently swallowed.
	Sport bool `json:"sport,omitempty"`
	// Ppv (Pay-Per-View) marks events that were sold individually —
	// wrestling specials, UFC numbered events, boxing. Orthogonal to
	// Sport: most PPV is sport, but the model itself describes a
	// delivery / commercial channel, not a quality tier. Kept out of
	// the Quality field so canonical tokens stay clean. Field name
	// uses Title-case ("Ppv" not "PPV") so reflect.FieldByName lookup
	// in TorrentInfo.MapField — which goes through strings.Title —
	// resolves correctly. Same convention as Avc, Sbs.
	Ppv bool `json:"ppv,omitempty"`
}

func (s *TorrentInfo) MapField(fieldType FieldType, val string) {
	ttor := reflect.TypeOf(s)
	torV := reflect.ValueOf(s)
	field := strings.Replace(strings.Title(strings.Replace(string(fieldType), "_", " ", -1)), " ", "", -1)
	v, ok := ttor.Elem().FieldByName(field)
	if !ok {
		return
	}
	//fmt.Printf("    field=%v, type=%+v, value=%v\n", field, v.Type, val)
	switch v.Type.Kind() {
	case reflect.Bool:
		torV.Elem().FieldByName(field).SetBool(true)
	case reflect.Int:
		clean, _ := strconv.ParseInt(val, 10, 64)
		torV.Elem().FieldByName(field).SetInt(clean)
	case reflect.Uint:
		clean, _ := strconv.ParseUint(val, 10, 64)
		torV.Elem().FieldByName(field).SetUint(clean)
	case reflect.String:
		torV.Elem().FieldByName(field).SetString(val)
	}
}

func (s *TorrentInfo) Map(ms Matches) {
	for _, m := range ms {
		s.MapField(m.FieldType, m.Content)
	}
}
