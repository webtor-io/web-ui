// Progress of an AI subtitle translation, as seen from the player.
//
// The translated .vtt is served incrementally: every GET returns the
// cues that are ready so far, and the response carries
// `X-Subtitle-Progress: <done>/<total>`. The player HEADs the same URL on
// a timer (HEAD never starts work) and reloads the <track> with a bumped
// &rev= whenever the count moves, so subtitles appear as they are
// translated instead of after the whole file is done.

export function parseProgress(header) {
    const m = /^(\d+)\/(\d+)$/.exec(String(header || '').trim());
    if (!m) return { done: 0, total: 0, final: false };
    const done = parseInt(m[1], 10);
    const total = parseInt(m[2], 10);
    // `0/0` is "the job has not counted the cues yet", not "done":
    // treating it as final would stop the poll before the first cue.
    return { done, total, final: total > 0 && done >= total };
}

// withRev busts the browser's cache for the partially-written track.
// The URL is signed, so the token and every other query parameter must
// survive untouched; only `rev` is added or replaced.
export function withRev(src, n) {
    const u = new URL(src, 'https://placeholder.invalid');
    u.searchParams.set('rev', String(n));
    if (/^https?:/i.test(src)) return u.toString();
    return u.pathname + u.search;
}

// pollProgress HEADs src every intervalMs, reports changes and stops on
// completion or on a non-200. Returns a stop() function — call it when
// the viewer selects another track or the player unmounts.
export function pollProgress(src, { fetchImpl = fetch, intervalMs = 3000, onProgress, onDone, onError } = {}) {
    let stopped = false;
    let last = -1;
    let timer = null;
    const tick = async () => {
        if (stopped) return;
        let res;
        try {
            res = await fetchImpl(src, { method: 'HEAD', cache: 'no-store' });
        } catch (e) {
            if (!stopped && onError) onError(0);
            return;
        }
        // Checked again after the await: stop() may have landed while the
        // request was in flight, and a callback firing into an unmounted
        // player would touch DOM that is no longer there.
        if (stopped) return;
        if (res.status !== 200) {
            if (onError) onError(res.status);
            return;
        }
        const p = parseProgress(res.headers.get('X-Subtitle-Progress'));
        if (p.done !== last) {
            last = p.done;
            if (onProgress) onProgress(p);
        }
        if (p.final) {
            if (onDone) onDone(p);
            return;
        }
        timer = setTimeout(tick, intervalMs);
    };
    timer = setTimeout(tick, 0);
    return () => { stopped = true; if (timer) clearTimeout(timer); };
}
