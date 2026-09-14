package api

import (
	"net/url"
	"path/filepath"
	"strings"
)

// TranslateURL appends the ~tr:<lang> mod to a subtitle URL, mirroring
// convertToVTT: the proxy resolves the inner chain itself, so any
// subtitle URL web-ui already builds (sidecar ~vtt, OpenSubtitles ~vi,
// user upload /ext/…~vtt) can be translated by suffixing it. names are
// character names for the service's glossary (query "names", CSV).
func TranslateURL(src, lang string, names []string) string {
	if src == "" || lang == "" {
		return ""
	}
	parsed, err := url.Parse(src)
	if err != nil {
		return ""
	}
	parts := strings.Split(parsed.Path, "/")
	name := parts[len(parts)-1]
	newName := strings.TrimSuffix(name, filepath.Ext(name)) + ".vtt"
	out := parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath() + "~tr:" + lang + "/" + url.PathEscape(newName)
	q := parsed.RawQuery
	if len(names) > 0 {
		v := url.Values{}
		v.Set("names", strings.Join(names, ","))
		if q != "" {
			q += "&"
		}
		q += v.Encode()
	}
	if q != "" {
		out += "?" + q
	}
	return out
}
