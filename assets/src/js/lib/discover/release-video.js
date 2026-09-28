// What a release's name says about its video: the codec, HDR, and whether it
// is Dolby Vision profile 5 (docs/discover.md, "Video switches").
//
// A guess from words, not a probe. The words are the ones release names
// use -- x265 / H.265 / HEVC, HDR10+ / DoVi / DV -- and a word counts only
// as a whole token: the characters around it are not letters or digits, so
// ".", " ", "-", "_", "[" and "(" are boundaries and "Tax265",
// "HEVCGroup", "HDRip" and "DVDRip" say nothing. Measured on the file paths
// of the films opened on Webtor in two weeks (24 062; not Discover's
// lists, whose names come from addons -- that error is not measured yet):
// HEVC by name is right 99.6% of the time and finds 78% of the HEVC files;
// HDR (PQ, the switch's rule) 97.9% and 79%. A fifth of HEVC files name no
// codec at all, so "unknown" is common, and it is never read as HEVC.
//
// Pure: no DOM, no storage. Discover's switches read it (playback.js).

// A boundary before a word, and one after it (a lookahead, so two words
// can share the separator between them).
const SEP = '(?:^|[^a-z0-9])';
const END = '(?=$|[^a-z0-9])';
const word = (body) => new RegExp(`${SEP}(?:${body})${END}`, 'i');

export const HEVC_RE = word('[hx][ .]?265|hevc');
export const AVC_RE = word('[hx][ .]?264|avc');
export const AV1_RE = word('av1');
// HDR, HDR10, HDR10+, HDR10P, HDR10Plus, HDRP -- PQ (HDR10) video. Not HDRip
// (a rip of an HDTV broadcast) and not HDTV.
export const PQ_RE = word('hdr|hdr10(?:\\+|p|plus)?|hdrp');
export const HLG_RE = word('hlg');
export const DV_RE = word('dv|dovi|dolby[ ._-]?vision');
// A Dolby Vision profile named outright: DV.P5, DoVi P8, P7.6. The profile
// must follow a boundary, so DDP5.1 (the audio) is not one.
export const DV_PROFILE_RE = new RegExp(`${SEP}(?:dv|dovi)?[ ._-]?p([578])(?:\\.\\d)?${END}`, 'i');
export const WEB_RE = word('web(?:[ ._-]?(?:dl|rip))?');
export const HYBRID_RE = word('hybrid');

// releaseText: what Discover knows by name about one stream -- the addon's
// name (Torrentio puts "4k DV | HDR10+" on its second line), its title
// (the torrent's name and, for a pack, the file's) and the file name an
// addon may hint.
export function releaseText(stream) {
    if (!stream || typeof stream !== 'object') return '';
    const hints = stream.behaviorHints && typeof stream.behaviorHints === 'object' ? stream.behaviorHints : {};
    return [stream.name, stream.title, hints.filename]
        .filter((s) => typeof s === 'string' && s)
        .join('\n');
}

// releaseVideo reads a release's text:
//   codec  'hevc' | 'avc' | 'av1' | 'unknown' -- 'unknown' where no codec is
//          named, and where two different ones are (a pack folder saying
//          x265 over a file saying x264: which is which is not ours to
//          guess, and an unknown release is always shown);
//   hdr    'pq' | 'hlg' | 'dv' | null -- HLG before PQ ("HLG.HDR.SDR" is an
//          HLG broadcast), PQ before DV ("DV.HDR10" has an HDR10 layer);
//          'dv' is Dolby Vision with no HDR word next to it;
//   dv5    Dolby Vision profile 5, which has no HDR10 or SDR layer: a
//          browser shows it with wrong colours whatever the route (the
//          transcoder never passes it through, and a re-encode keeps its
//          colours wrong). By name: the profile said outright (P5), or
//          Dolby Vision with no HDR word from a WEB source that is not a
//          HYBRID -- the streaming services' DV5; a hybrid is a DV layer
//          grafted onto a Blu-ray's HDR10, profile 8. Measured: 4 of the
//          5 names this rule marked were DV5, and it found 4 of 6 DV5
//          files; plain "DV without HDR" was DV5 only 4 times in 19.
export function releaseVideo(text) {
    const s = String(text || '');
    const named = [HEVC_RE.test(s) && 'hevc', AVC_RE.test(s) && 'avc', AV1_RE.test(s) && 'av1'].filter(Boolean);
    const codec = named.length === 1 ? named[0] : 'unknown';
    const pq = PQ_RE.test(s);
    const hlg = HLG_RE.test(s);
    const dv = DV_RE.test(s);
    let dv5 = false;
    if (dv) {
        const m = s.match(DV_PROFILE_RE);
        const profile = m ? Number(m[1]) : null;
        dv5 = profile === 5 || (profile === null && !pq && !hlg && WEB_RE.test(s) && !HYBRID_RE.test(s));
    }
    return {
        codec,
        hdr: hlg ? 'hlg' : pq ? 'pq' : dv ? 'dv' : null,
        dv5,
    };
}
