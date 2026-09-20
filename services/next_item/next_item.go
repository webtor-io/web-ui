// Package next_item answers one question: the viewer finished this file --
// what plays next? It is pure: the caller brings the episode rows and the
// directory listing, and gets a pick or nil. See
// docs/superpowers/specs/2026-09-20-next-episode-design.md.
//
// Two rules, by what the file is:
//
//   - A video that is an enriched EPISODE follows the series' own order
//     (season, episode) among the episodes THIS torrent carries. 86% of
//     viewers who finish an episode open the next one by hand (watch_history,
//     2026-09-20); the file names are not consulted -- enrichment covers that
//     class (1125 enriched vs 1073 episode-looking names in 28 days).
//   - AUDIO follows the directory: the next audio file in natural order, the
//     way an album or an audiobook is laid out. There is no metadata to ask.
//
// A video that is not an episode has no "next": a directory of films is not a
// playlist, and playing the next one unasked would be a guess.
package next_item

import (
	"path"
	"sort"
	"strings"
	"unicode"
)

type Kind string

const (
	KindEpisode Kind = "episode"
	KindTrack   Kind = "track"
)

// Episode is one episode row of the series, already narrowed to this
// resource. Path is empty for an episode the torrent has no file for.
type Episode struct {
	Season  int
	Episode int
	Path    string
	Title   string
}

// File is one file of the current file's directory.
type File struct {
	ID    string
	Path  string
	Name  string
	Audio bool
}

type Pick struct {
	Kind    Kind
	Path    string
	Season  int // episodes only
	Episode int // episodes only
	Title   string
}

// NextEpisode returns the episode after the one at currentPath, or nil.
//
// Specials (season 0) never bridge to the regular seasons or back: S00E05 is
// not what follows S01E10 in any order a viewer means. A file that holds two
// episodes (S01E01-E02) is followed by what comes after its LAST episode, and
// an episode without a file is skipped rather than ending the run.
func NextEpisode(currentPath string, episodes []Episode) *Pick {
	var cur *Episode
	for i := range episodes {
		e := &episodes[i]
		if e.Path != currentPath {
			continue
		}
		if cur == nil || after(e, cur) {
			cur = e
		}
	}
	if cur == nil {
		return nil
	}
	var next *Episode
	for i := range episodes {
		e := &episodes[i]
		if e.Path == "" || e.Path == currentPath || !after(e, cur) {
			continue
		}
		if (e.Season == 0) != (cur.Season == 0) {
			continue
		}
		if next == nil || after(next, e) {
			next = e
		}
	}
	if next == nil {
		return nil
	}
	return &Pick{Kind: KindEpisode, Path: next.Path, Season: next.Season, Episode: next.Episode, Title: next.Title}
}

func after(a, b *Episode) bool {
	if a.Season != b.Season {
		return a.Season > b.Season
	}
	return a.Episode > b.Episode
}

// NextTrack returns the audio file that follows currentPath in its own
// directory, in natural order ("2 - x" before "10 - x"), or nil at the end.
// Files of other directories are ignored even if the caller passes them:
// "CD1/12" is not followed by "CD2/01" in v1.
func NextTrack(currentPath string, files []File) *Pick {
	dir := path.Dir(currentPath)
	var tracks []File
	for _, f := range files {
		if f.Audio && path.Dir(f.Path) == dir {
			tracks = append(tracks, f)
		}
	}
	sort.SliceStable(tracks, func(i, j int) bool { return naturalLess(tracks[i].Name, tracks[j].Name) })
	for i, f := range tracks {
		if f.Path == currentPath && i+1 < len(tracks) {
			n := tracks[i+1]
			return &Pick{Kind: KindTrack, Path: n.Path, Title: strings.TrimSuffix(n.Name, path.Ext(n.Name))}
		}
	}
	return nil
}

// naturalLess compares strings with digit runs read as numbers, case
// folded. Leading zeros do not matter ("02" == "2"), and the plain string
// order breaks ties so the result is total.
func naturalLess(a, b string) bool {
	ar, br := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	i, j := 0, 0
	for i < len(ar) && j < len(br) {
		if unicode.IsDigit(ar[i]) && unicode.IsDigit(br[j]) {
			si := i
			for i < len(ar) && unicode.IsDigit(ar[i]) {
				i++
			}
			sj := j
			for j < len(br) && unicode.IsDigit(br[j]) {
				j++
			}
			na := strings.TrimLeft(string(ar[si:i]), "0")
			nb := strings.TrimLeft(string(br[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if ar[i] != br[j] {
			return ar[i] < br[j]
		}
		i++
		j++
	}
	if len(ar)-i != len(br)-j {
		return len(ar)-i < len(br)-j
	}
	return a < b
}
