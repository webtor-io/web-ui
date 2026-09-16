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
//
// `X-Subtitle-Status` is the service's own verdict on a live run, and it
// outranks the counts because it knows two things they cannot say:
//
//   - `done` — the source ended and everything in it was translated. A
//     live run can finish without a final artifact (a seeked session
//     caches nothing), so there is no other way to tell "finished" from
//     "the playlist has not grown yet in the last three seconds", and
//     the live rule would otherwise poll such a run until the timeout.
//   - `stopped` — the run ended incomplete (`source_gone`, `too_large`).
//     The counts look exactly like a job that is merely behind.
//
// Absent (every response before the service shipped it, and every batch
// run) the counts decide alone, exactly as before.
//
// `X-Subtitle-Pending-From: <seconds>` is the last of the four, and the
// only one about the *film* rather than about the job: the start, in movie
// time, of the earliest untranslated cue the viewer can still meet in this
// run. It is what lets the player say "the translation is behind where you
// are" — the counts cannot, because a run 200 cues from the end may be
// hours ahead of the playhead or ten seconds behind it. Absent when
// nothing is pending ahead, when the source is not live, and on every
// service that predates it; absence is always read as "no banner", never
// as "behind".
export const STATUS_DONE = 'done';
export const STATUS_STOPPED = 'stopped';

// parsePendingFrom is deliberately strict about what counts as a number.
// Number('') is 0 and Number(' ') is 0 too, and a 0 here means "the
// earliest untranslated cue starts at the top of the film", which is a
// claim to pause the viewer on. An empty or unparseable header is no
// claim at all.
function parsePendingFrom(header) {
    if (header === null || header === undefined) return null;
    const s = String(header).trim();
    if (!s) return null;
    const n = Number(s);
    // Negative is not a movie time, and neither is NaN or Infinity.
    if (!Number.isFinite(n) || n < 0) return null;
    return n;
}

export function parseProgress(header, live = false, status = '', pendingFrom = null) {
    const isLive = Boolean(live);
    const st = String(status || '').trim().toLowerCase();
    const pending = parsePendingFrom(pendingFrom);
    const m = /^(\d+)\/(\d+)$/.exec(String(header || '').trim());
    if (!m) return { done: 0, total: 0, final: st === STATUS_DONE, live: isLive, pendingFrom: pending };
    const done = parseInt(m[1], 10);
    const total = parseInt(m[2], 10);
    // `0/0` is "the job has not counted the cues yet", not "done". A live
    // source (X-Subtitle-Live) is never done either: its total grows with
    // the playlist, so done == total only means "caught up for now" --
    // unless the service says the run finished, which is the one thing
    // the counts cannot report.
    if (st === STATUS_DONE) return { done, total, final: true, live: isLive, pendingFrom: pending };
    return { done, total, final: !isLive && total > 0 && done >= total, live: isLive, pendingFrom: pending };
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

// QUEUE_HINT_MS is how long a run may answer `0/0` before the chip stops
// pretending to be at 0% and says it is waiting instead. `0/0` means "the
// job has not counted the cues yet", which covers both "starting" and
// "queued behind every other translation on the service", and thirty
// seconds is well past the first and only reachable by the second.
//
// It is a display rule, not a failure: the poll keeps running, the
// deadline is untouched, and the first real count clears it.
export const QUEUE_HINT_MS = 30 * 1000;

// progressText is what the AI chip's `.tr-progress` says for one progress
// report: the text of the span and the i18n key (plus its argument) for its
// title. Here rather than in Player.jsx because it is the whole of the
// chip's vocabulary and the only part of it worth testing on its own --
// three states that are easy to get into the wrong order.
export function progressText(p) {
    // Waiting outranks the count, because the count is what it is
    // contradicting: `· 0%` on a queued job reads as a translation that
    // has stalled at the start.
    if (p.queued) return { text: '· …', key: 'player.subtitleTranslationQueued', args: [] };
    // A live source has no denominator worth a percent: the playlist grows
    // with the transcode. Show the count and say so in the title.
    if (p.live) return { text: `· ${p.done}`, key: 'player.subtitleTranslatingLive', args: [] };
    const pct = p.total > 0 ? Math.round((100 * p.done) / p.total) : 0;
    return { text: `· ${pct}%`, key: 'player.subtitleTranslating', args: [pct] };
}

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
// Reporting `done` or an error is terminal: the run marks itself stopped
// before the callback, so a later suspend()/resume() pair cannot re-arm a
// run that already finished or failed. Without that there was a fourth,
// implicit state — "finished, not stopped" — in which a resume would
// re-poll a completed job and emit a second error for the timeout path.
// It was unreachable through Player.jsx only because the player nulls its
// stop ref first, which is a caller-side invariant this module neither
// stated nor enforced.
//
// stop() also carries suspend() and resume(): the run goes to sleep with
// the video (pause, hidden tab) and wakes with it. Sleeping is not
// stopping and not failing — nothing is reported, so the chip keeps its
// count, and the same run continues afterwards. It matters because the
// HEAD is what tells the translate service somebody is still watching;
// for a live source it is also what keeps the transcoder session and its
// FFmpeg run alive, and nobody should pay for a film nobody is watching.
//
// stop() carries kick() too: a session seek jumps the video to a new
// position, and without it the first cues for that position wait out
// whatever is left of the 3 s poll interval on top of the service's own
// translation lag, and then the caller's own reload throttle
// (`TRACK_RELOAD_INTERVAL_MS` in Player.jsx) on top of that. kick() runs a
// tick right away instead of waiting for the interval, and marks the next
// changed report with `forceReload: true` so the caller's reload can
// bypass its throttle for that one swap — "next", not "this one", because
// the immediate tick usually still finds the pre-seek count: the service
// needs a moment to catch up, and the mark has to survive until whichever
// later tick brings the first real change. A no-op once the run is
// stopped or asleep: kicking a finished run polls nothing, and kicking a
// suspended one would defeat the reason it is asleep — nobody is
// watching.
// Properties on the returned function rather than an object, so every
// caller that just calls stop() keeps working.
//
// onTick is the other callback, and the difference is the whole reason it
// exists: onProgress fires only when the report changed, onTick fires on
// every successful 200. The catching-up banner compares the run against
// the playhead, and the playhead moves whether or not the counts do — a
// translation standing still while the film runs on is exactly the case
// it is there to catch. It runs after the change and queue blocks (so a
// tick's onProgress is always the earlier call) and before the terminal
// ones, because a run that is about to be reported done or stopped should
// not first be reported as trailing.
export function pollProgress(src, { fetchImpl = fetch, intervalMs = 3000, timeoutMs = POLL_TIMEOUT_MS, queuedAfterMs = QUEUE_HINT_MS, onProgress, onTick, onDone, onError } = {}) {
    let stopped = false;
    let suspended = false;
    let suspendedAt = 0;
    let last = -1;
    let lastTotal = -1;
    let lastLive = null;
    // Undefined rather than null: null is a value this field takes (the
    // header was absent), so the "nothing seen yet" marker has to be
    // something else, or the first report of an absent header would not
    // count as a change.
    let lastPendingFrom;
    // When this run started answering `0/0`, and whether the chip has been
    // told about it. Both reset on the first real count, so a job that goes
    // back to reporting nothing could say "waiting" again -- it would be
    // waiting again.
    let queuedSince = 0;
    let queuedReported = false;
    // Set by kick(), consumed by the next onProgress that reports an
    // actual change (see the change-detection block below). It survives
    // ticks that find nothing new: a kicked HEAD landing before the
    // service has caught up must not lose the mark, or the reload it was
    // meant to unthrottle goes back to waiting out the 15 s window.
    let pendingKick = false;
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
            stopped = true;
            if (onError) onError('timeout');
            return;
        }
        let res;
        try {
            res = await fetchImpl(src, { method: 'HEAD', cache: 'no-store' });
        } catch (e) {
            // A throw on a dead generation is a request that was in flight
            // when the video paused: the run is asleep, not failed, and
            // resume() must still be able to wake it.
            if (stopped || myGen !== gen) return;
            stopped = true;
            if (onError) onError(0);
            return;
        }
        // Checked again after the await: stop() or suspend() may have
        // landed while the request was in flight, and a callback firing
        // into an unmounted player would touch DOM that is no longer there.
        if (stopped || myGen !== gen) return;
        if (res.status !== 200) {
            stopped = true;
            if (onError) onError(res.status);
            return;
        }
        const status = String(res.headers.get('X-Subtitle-Status') || '').trim().toLowerCase();
        const p = parseProgress(
            res.headers.get('X-Subtitle-Progress'),
            res.headers.get('X-Subtitle-Live') === '1',
            status,
            res.headers.get('X-Subtitle-Pending-From'),
        );
        // pendingFrom is part of the change test, not just a passenger on
        // it: after a seek the run's frontier moves while the counts stand
        // still (the same cues, a different place in the film), and that
        // move is the one the banner is reading.
        if (p.done !== last || p.total !== lastTotal || p.live !== lastLive || p.pendingFrom !== lastPendingFrom) {
            // A cue that was not there before is proof the job is alive,
            // which is the whole content of the cap. Only a live source
            // gets the extension: a batch run keeps its absolute deadline,
            // because its end is minutes away and not film-length.
            if (p.live && p.done !== last) deadline = Date.now() + timeoutMs;
            last = p.done;
            lastTotal = p.total;
            lastLive = p.live;
            lastPendingFrom = p.pendingFrom;
            if (onProgress) {
                if (pendingKick) {
                    pendingKick = false;
                    onProgress({ ...p, forceReload: true });
                } else {
                    onProgress(p);
                }
            }
        }
        // `0/0` for long enough is a job waiting for a slot, not a job
        // starting. Reported through onProgress like any other change,
        // once, and cleared by the first count -- which reaches the chip
        // through the branch above, since either number moving is a change.
        if (p.done === 0 && p.total === 0) {
            if (!queuedSince) queuedSince = Date.now();
            if (!queuedReported && Date.now() - queuedSince >= queuedAfterMs) {
                queuedReported = true;
                if (onProgress) onProgress({ ...p, queued: true });
            }
        } else {
            queuedSince = 0;
            queuedReported = false;
        }
        // Every 200, changed or not. See the note on the option above: the
        // banner's question is about the playhead, which moves on its own.
        if (onTick) onTick(p);
        // After onProgress and before the final check: the counts in a
        // "stopped" response are the last ones there will ever be, so the
        // chip is painted with them first and the run is reported dead
        // second. Terminal like every other onError path -- one report,
        // and resume() cannot wake it.
        if (status === STATUS_STOPPED) {
            stopped = true;
            if (onError) onError(STATUS_STOPPED);
            return;
        }
        if (p.final) {
            stopped = true;
            if (onDone) onDone(p);
            return;
        }
        // Re-checked after onProgress: a callback may suspend the run (the
        // player does exactly that once it learns from the first response
        // whether the source is live), and scheduling here regardless would
        // leave a dead-generation timer behind for resume() to race with.
        if (stopped || myGen !== gen) return;
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
        // timeout the moment playback resumes. The queue hint is measured
        // the same way -- a run asleep is not a run queued.
        const slept = Date.now() - suspendedAt;
        deadline += slept;
        if (queuedSince) queuedSince += slept;
        timer = setTimeout(() => tick(gen), 0);
    };
    // A no-op while stopped (nothing left to kick) or suspended (kicking a
    // sleeping run would spend the HEAD suspend() exists to save). gen++
    // discards whatever tick is in flight or scheduled — same trick as
    // suspend() — so kick() never races a second timer chain onto the run:
    // the one tick it starts is the only one that gets to schedule the
    // next.
    stop.kick = () => {
        if (stopped || suspended) return;
        pendingKick = true;
        gen++;
        if (timer) clearTimeout(timer);
        timer = setTimeout(() => tick(gen), 0);
    };
    return stop;
}
