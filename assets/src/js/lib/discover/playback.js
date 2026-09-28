// Discover's video switches -- HEVC, HDR and 4K above a title's releases --
// and what they are allowed to promise (docs/discover.md, "Video switches").
//
// Three questions, each answered by what actually decides it, never by what
// kind of deployment this is:
//
//   "Does this browser decode HEVC?"  / "... decode HDR (PQ)?"
//       The browser's own answer (decode-declaration.js decodedTokens: the
//       same probe and tokens the page declares to the transcoder). Decides
//       the HEVC and HDR switches' defaults and whether turning one on
//       warns. A browser that decodes HEVC gets HEVC releases shown; one
//       that does not gets them hidden until the viewer asks, because here
//       they are converted on our side -- slower, and 4K not at all.
//   "Does 4K HEVC play here?"
//       Only where the transcoder passes HEVC through (its own answer,
//       window._passthrough, services/transcodercaps) AND this page sends
//       it a declaration that covers 4K Main10 -- the transcoder's own rule
//       for a 4K HEVC source (content-transcoder route.go). Where it does,
//       the 4K switch is gone and 4K is simply shown; everywhere else the
//       switch stays with its warning, which is true there.
//
// A check that has not answered is never read as a "no": a browser whose
// probe has not answered yet keeps HEVC and HDR shown (today's list) and is
// not warned, and on a page that declares, a transcoder whose answer is
// unknown gets a 4K warning that says the check did not happen, not that 4K
// is off. A page that does not declare never asks: its sessions take the
// old route whatever the transcoder answers. The viewer's own choice
// (discover-prefs in localStorage, written only when they flip a switch) is
// kept over any default.
//
// Pure: everything comes in as arguments. StreamModal renders it;
// DiscoverApp gathers the inputs (playbackContext).

export const HEVC_TOKENS = ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160'];
export const UHD_TOKEN = 'hevc10-2160';
export const PQ_TOKEN = 'hdr-pq';

// transcoderAnswer: the transcoder's last answer on HEVC passthrough as the
// page got it -- 'on', 'off', or 'unknown' for anything else (no answer
// yet, no value on the page).
export function transcoderAnswer(win) {
    try {
        const v = win._passthrough && win._passthrough.hevc;
        return v === 'on' || v === 'off' ? v : 'unknown';
    } catch (e) {
        return 'unknown';
    }
}

// browserDecodesHevc: true where the browser decodes any HEVC the
// transcoder would hand it (a Main10 or 4K token covers Main 1080p, the
// transcoder's `covers`), false where it answered and decodes none, null
// where it has not answered.
export function browserDecodesHevc(decodes) {
    if (!Array.isArray(decodes)) return null;
    return HEVC_TOKENS.some((t) => decodes.includes(t));
}

// browserDecodesPq: the same for HDR (PQ). The probe asks PQ only of a
// browser that decodes HEVC; one that decodes none has answered "no".
export function browserDecodesPq(decodes) {
    if (!Array.isArray(decodes)) return null;
    return decodes.includes(PQ_TOKEN);
}

// uhdPlaysHere: does 4K HEVC play on this page -- the transcoder passes HEVC
// through and the page declares 4K Main10 (9 in 10 HEVC sources over 1080p
// are Main10; a browser with 4K Main only would be turned away from them).
export function uhdPlaysHere(caps, declared) {
    return caps === 'on' && Array.isArray(declared) && declared.includes(UHD_TOKEN);
}

// uhdWarningKey: why 4K may not play here, for the 4K switch's warning.
//   - this page does not declare: its sessions take the old route whatever
//     the transcoder answers, and there 4K is converted, which is off --
//     the warning Discover has always shown, true there. The transcoder's
//     answer is not asked: until it is known (every page, before the
//     transcoder has GET /capabilities) nothing on this page changes;
//   - the transcoder's answer is not known yet, or the browser has not
//     answered: the check did not happen -- say so, not "switched off";
//   - the transcoder passes HEVC through and this browser declared, without
//     4K Main10: this browser is why;
//   - the transcoder converts: the old warning, as for a page that does not
//     declare.
export function uhdWarningKey(caps, part, declared) {
    if (!part) return 'discover.warning4kBody';
    if (caps === 'unknown') return 'discover.warning4kBodyUnchecked';
    if (caps === 'on') {
        return Array.isArray(declared) ? 'discover.warning4kBodyNoHevc' : 'discover.warning4kBodyUnchecked';
    }
    return 'discover.warning4kBody';
}

// isHdrRelease: the releases the HDR switch hides -- PQ by name, and Dolby
// Vision without an HDR word (profiles 8 and 7 carry an HDR10 layer). Not
// profile 5, whatever else the name says: it has no HDR10 layer to decode,
// and it is badged for every browser instead of hidden by this switch
// (StreamModal). Not HLG: it is made to be watchable on an SDR screen.
export function isHdrRelease(video) {
    return !!video && !video.dv5 && (video.hdr === 'pq' || video.hdr === 'dv');
}

// switchStates: which releases are shown, from the viewer's choices and the
// defaults. `prefs` is discover-prefs: showHevc / showHdr / show4k are
// booleans only where the viewer set them.
export function switchStates({ caps, decodes, part, declared, prefs }) {
    const p = prefs || {};
    const hevcDecoded = browserDecodesHevc(decodes);
    const pqDecoded = browserDecodesPq(decodes);
    const uhdPlays = uhdPlaysHere(caps, declared);
    return {
        hevc: {
            shown: typeof p.showHevc === 'boolean' ? p.showHevc : hevcDecoded !== false,
            warns: hevcDecoded === false,
        },
        hdr: {
            shown: typeof p.showHdr === 'boolean' ? p.showHdr : pqDecoded !== false,
            warns: pqDecoded === false,
        },
        // Where 4K plays there is no switch and no choice to keep: show4k was
        // the answer to "show 4K that will not play here?". It stays in
        // storage and comes back with the switch if 4K stops playing.
        uhd: {
            switchable: !uhdPlays,
            shown: uhdPlays || p.show4k === true,
            warningKey: uhdWarningKey(caps, part, declared),
        },
    };
}

// hiddenBy: the switches that hide one release (`row` = {video, uhd}), in
// the order they stand: HEVC, HDR, 4K.
export function hiddenBy(row, states) {
    const out = [];
    if (row.video.codec === 'hevc' && !states.hevc.shown) out.push('hevc');
    if (isHdrRelease(row.video) && !states.hdr.shown) out.push('hdr');
    if (row.uhd && !states.uhd.shown) out.push('uhd');
    return out;
}

// switchCounts: how many releases each switch is about, before any
// filtering -- the number in its label, and whether it is shown at all.
export function switchCounts(rows) {
    const n = { hevc: 0, hdr: 0, uhd: 0 };
    for (const r of rows) {
        if (r.video.codec === 'hevc') n.hevc++;
        if (isHdrRelease(r.video)) n.hdr++;
        if (r.uhd) n.uhd++;
    }
    return n;
}

const ALL_HIDDEN_KEY = {
    hevc: 'discover.allHevcStreams',
    hdr: 'discover.allHdrStreams',
    uhd: 'discover.all4kStreams',
};

// emptyStateKey: when the switches hide every release of a non-empty list,
// the text that says so -- naming the one switch where one is enough to
// hide them all, else all of them. null when something is left to show.
export function emptyStateKey(rows, states) {
    if (!rows.length) return null;
    const by = new Set();
    for (const r of rows) {
        const h = hiddenBy(r, states);
        if (!h.length) return null;
        for (const s of h) by.add(s);
    }
    return by.size === 1 ? ALL_HIDDEN_KEY[[...by][0]] : 'discover.allHiddenStreams';
}

// playbackContext gathers the inputs from the page: the transcoder's answer,
// the browser's, and what this page declares. `dd` is
// decode-declaration.js (passed in, so this module stays pure). Never
// throws: whatever fails is an unanswered check.
export function playbackContext(win, dd) {
    const out = { caps: 'unknown', decodes: null, part: false, declared: null };
    try { out.caps = transcoderAnswer(win); } catch (e) { /* unknown */ }
    try { out.decodes = dd.decodedTokens(win); } catch (e) { /* not answered */ }
    try { out.part = dd.takesPart(win) === true; } catch (e) { /* not taking part */ }
    try { out.declared = dd.declaredTokens(win); } catch (e) { /* not answered */ }
    return out;
}
