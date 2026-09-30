// A fragment loaded again and again, said and stopped (2026-09-30).
//
// hls.js loads a fragment, appends it, then finds it is not in the buffer
// and loads it again -- two neighbours, N and N+1, for as long as the page
// is open: ~2.7 requests a second each, answered by the browser's cache
// (the transcoder sees 304s), 6-8 sessions a day, up to 33k requests one.
// Nothing errors, so no guard (passthrough.js) and no network recovery
// (network-recovery.js) sees it, and the viewer sits on a frozen picture.
// One cause is known: Chrome silently drops a whole passthrough fMP4
// fragment in which a leading picture shares the keyframe's timestamp
// (docs/player.md, "A fragment loaded again and again"; the transcoder
// makes those timestamps unique). Others are not: a loop on iOS (WebKit)
// was on the audio's first fragments.
//
// So: the same fragment -- type, level, sn -- loaded LOOP_LOADS times
// within LOOP_WINDOW_MS is a loop, whatever the cause, and onLoop is told
// once. A new manifest (a session seek, a restart: MANIFEST_LOADING) starts
// the count again -- a new run numbers its fragments from 0 again. A
// fragment a viewer's seeks bring back, or a recovery reloads, stays well
// under the count: those are one or two loads.

export const LOOP_LOADS = 6;
export const LOOP_WINDOW_MS = 60000;

// createFragmentLoopWatch watches one hls.js instance for its life. onLoop
// gets { type, sn, level, loads, windowMs } once.
export function createFragmentLoopWatch(hls, Hls, { onLoop = () => {}, now = () => Date.now(),
    loads = LOOP_LOADS, windowMs = LOOP_WINDOW_MS } = {}) {
    let seen = new Map();
    let fired = false;

    const onLoaded = (event, data) => {
        const f = data && data.frag;
        if (fired || !f || typeof f.sn !== 'number') return;
        const key = `${f.type}:${f.level}:${f.sn}`;
        const t = now();
        const times = (seen.get(key) || []).filter((x) => t - x < windowMs);
        times.push(t);
        seen.set(key, times);
        if (times.length < loads) return;
        fired = true;
        try {
            onLoop({ type: f.type, sn: f.sn, level: f.level, loads: times.length, windowMs });
        } catch (e) { /* the page goes on */ }
    };
    const onManifest = () => { seen = new Map(); };
    const listeners = [
        [Hls.Events.FRAG_LOADED, onLoaded],
        [Hls.Events.MANIFEST_LOADING, onManifest],
    ];
    const stop = () => {
        for (const [ev, fn] of listeners) hls.off(ev, fn);
        hls.off(Hls.Events.DESTROYING, stop);
    };
    for (const [ev, fn] of listeners) hls.on(ev, fn);
    hls.on(Hls.Events.DESTROYING, stop);
    return { stop, get fired() { return fired; } };
}

// loopGivesUp is whether a loop gives this passthrough up to the old route
// (reason fragment_loop): only where the old route plays the file. Over
// 1080 (the -2160 classes) the transcoder refuses it and the viewer would
// read that the browser cannot show 4K HEVC -- it can, it dropped one
// fragment -- and the memory would keep the file refused for 7 days. A
// frozen picture the viewer can seek past is the lesser harm there.
export function loopGivesUp(video) {
    const d = (video && video.dataset) || {};
    return d.videoRoute === 'passthrough' && !/-2160$/.test(d.videoClass || '');
}
