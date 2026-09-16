// Progress of an AI subtitle translation, as seen from the player.
//
// The translated .vtt is served incrementally: every GET returns the
// cues that are ready so far, and the response carries
// `X-Subtitle-Progress: <done>/<total>`. The player HEADs the same URL on
// a timer (HEAD never starts work) and reloads the <track> with a bumped
// &rev= whenever the count moves, so subtitles appear as they are
// translated instead of after the whole file is done.
//
// A response can also carry `X-Subtitle-Live: 1`, meaning the source
// playlist is still growing (embedded-track translation): `total` is a
// snapshot, not a ceiling, so `done == total` only means "caught up for
// now", not final. When the header disappears the normal rule applies
// again.

export function parseProgress(header, live = false) {
    const isLive = Boolean(live);
    const m = /^(\d+)\/(\d+)$/.exec(String(header || '').trim());
    if (!m) return { done: 0, total: 0, final: false, live: isLive };
    const done = parseInt(m[1], 10);
    const total = parseInt(m[2], 10);
    // `0/0` is "the job has not counted the cues yet", not "done". A live
    // source (X-Subtitle-Live) is never done either: its total grows with
    // the playlist, so done == total only means "caught up for now".
    return { done, total, final: !isLive && total > 0 && done >= total, live: isLive };
}

// POLL_TIMEOUT_MS bounds a single translation run. A job that neither
// finishes nor fails leaves the player polling for the life of the page,
// so a run that goes this long without a sign of life is treated as gone
// rather than slow.
//
// What "this long" is measured from depends on the source. For a batch
// source it is the start of the run: fifteen minutes is well past the
// longest observed one. A live source (X-Subtitle-Live) finishes only
// when the transcode does, i.e. after the whole film, so against it the
// same number is an inactivity cap — fifteen minutes with no new
// translated cue. Answering is not progress: a live job that keeps
// returning the same count still times out.
export const POLL_TIMEOUT_MS = 15 * 60 * 1000;

// A scheme ("https:", "blob:") with an authority. Anything else is
// either protocol-relative, root-relative or relative to the page.
const ABSOLUTE = /^[a-z][a-z0-9+.-]*:\/\//i;

// withRev busts the browser's cache for the partially-written track.
// The URL is signed, so the token and every other query parameter must
// survive untouched; only `rev` is added or replaced. The placeholder
// base is only there to get a parser: whatever shape the input had is
// put back, because a protocol-relative src rewritten as https:// would
// break on an http page and a relative one rewritten as /path would
// point at the site root.
export function withRev(src, n) {
    const s = String(src || '');
    const u = new URL(s, 'https://placeholder.invalid');
    u.searchParams.set('rev', String(n));
    if (ABSOLUTE.test(s)) return u.toString();
    if (s.startsWith('//')) return u.toString().replace(/^https?:/i, '');
    if (s.startsWith('/')) return u.pathname + u.search + u.hash;
    const q = s.search(/[?#]/);
    return (q < 0 ? s : s.slice(0, q)) + u.search + u.hash;
}

// pollProgress HEADs src every intervalMs, reports changes and stops on
// completion or on a non-200. Returns a stop() function — call it when
// the viewer selects another track or the player unmounts.
//
// stop() also carries suspend() and resume(): the run goes to sleep with
// the video (pause, hidden tab) and wakes with it. Sleeping is not
// stopping and not failing — nothing is reported, so the chip keeps its
// count, and the same run continues afterwards. It matters because the
// HEAD is what tells the translate service somebody is still watching;
// for a live source it is also what keeps the transcoder session and its
// FFmpeg run alive, and nobody should pay for a film nobody is watching.
// Properties on the returned function rather than an object, so every
// caller that just calls stop() keeps working.
export function pollProgress(src, { fetchImpl = fetch, intervalMs = 3000, timeoutMs = POLL_TIMEOUT_MS, onProgress, onDone, onError } = {}) {
    let stopped = false;
    let suspended = false;
    let suspendedAt = 0;
    let last = -1;
    let lastLive = null;
    let timer = null;
    // Which chain of ticks is the live one. suspend() bumps it, so a
    // request already in flight when the video paused lands on a dead
    // generation: it reports nothing and, more importantly, does not
    // schedule the tick after it.
    let gen = 0;
    let deadline = Date.now() + timeoutMs;
    const tick = async (myGen) => {
        if (stopped || myGen !== gen) return;
        if (Date.now() >= deadline) {
            // Reported as an error, not silence: the viewer is looking at
            // a count that stopped moving, and a run that outlives the cap
            // is a failure worth counting.
            if (onError) onError('timeout');
            return;
        }
        let res;
        try {
            res = await fetchImpl(src, { method: 'HEAD', cache: 'no-store' });
        } catch (e) {
            if (!stopped && myGen === gen && onError) onError(0);
            return;
        }
        // Checked again after the await: stop() or suspend() may have
        // landed while the request was in flight, and a callback firing
        // into an unmounted player would touch DOM that is no longer there.
        if (stopped || myGen !== gen) return;
        if (res.status !== 200) {
            if (onError) onError(res.status);
            return;
        }
        const p = parseProgress(res.headers.get('X-Subtitle-Progress'), res.headers.get('X-Subtitle-Live') === '1');
        if (p.done !== last || p.live !== lastLive) {
            // A cue that was not there before is proof the job is alive,
            // which is the whole content of the cap. Only a live source
            // gets the extension: a batch run keeps its absolute deadline,
            // because its end is minutes away and not film-length.
            if (p.live && p.done !== last) deadline = Date.now() + timeoutMs;
            last = p.done;
            lastLive = p.live;
            if (onProgress) onProgress(p);
        }
        if (p.final) {
            if (onDone) onDone(p);
            return;
        }
        timer = setTimeout(() => tick(myGen), intervalMs);
    };
    timer = setTimeout(() => tick(gen), 0);
    const stop = () => {
        stopped = true;
        if (timer) clearTimeout(timer);
        timer = null;
    };
    stop.suspend = () => {
        if (stopped || suspended) return;
        suspended = true;
        suspendedAt = Date.now();
        gen++;
        if (timer) clearTimeout(timer);
        timer = null;
    };
    stop.resume = () => {
        if (stopped || !suspended) return;
        suspended = false;
        // The cap measures a job that stopped producing, not a viewer who
        // stopped watching: an hour on pause must not come back as a
        // timeout the moment playback resumes.
        deadline += Date.now() - suspendedAt;
        timer = setTimeout(() => tick(gen), 0);
    };
    return stop;
}
