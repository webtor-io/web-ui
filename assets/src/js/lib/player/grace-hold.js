// The grace popup holds the film (owner, 2026-09-26: "я бы всё-таки
// останавливал видео при появлении попапа"). Before, the film played on under
// the popup, and the viewer read the offer while the minutes past the free
// window went by at the cap, unchosen.
//
// start(): the popup is up. A film that plays is paused, and whatever starts
// it again behind the popup -- a session seek's new run (session-seek.js
// asks holds() first and does not play), the element's own `autoplay` once
// that run can play (hls.js reloads the element, and a reload re-arms it:
// Chrome, 2026-09-26, play and pause in the same millisecond), the subtitle
// catch-up letting go, the embed's player_play -- is paused back on its
// `play` event and counted as playback wanted. release(): the viewer's
// answer ("continue at N Mbps", the close, or Play itself). It resumes the
// film only if the popup held playback: a viewer who had paused before the
// popup came up, or a seek that lands paused, stays paused -- unless the
// answer IS Play (`play: true`). The trial link is no answer: it opens a new
// tab, the popup stays, and nothing resumes behind it. dispose(): the player
// goes (the next file, a teardown) or the viewer moved on (Next): nothing
// resumes, ever.
//
// While it holds playback the element carries data-grace-cta-hold: the pause
// is the page's, not the viewer's, and the resource page's transfer status
// keeps reading the player as playing (lib/playerActivity.js) -- hls.js keeps
// filling the buffer at the cap meanwhile, and the viewer is reading the
// popup, not gone.
export function createGraceHold(video) {
    let active = false;
    let resume = false;
    const pause = () => {
        if (typeof video.pause === 'function') video.pause();
    };
    const want = () => {
        resume = true;
        if (video.dataset) video.dataset.graceCtaHold = '';
    };
    // On the `play` event rather than around play(): every path that starts
    // the element passes through it, ours or not.
    function onPlay() {
        if (!active) return;
        want();
        pause();
    }
    const drop = () => {
        active = false;
        resume = false;
        video.removeEventListener('play', onPlay);
        if (video.dataset) delete video.dataset.graceCtaHold;
    };
    return {
        start() {
            if (active) return;
            active = true;
            video.addEventListener('play', onPlay);
            if (!video.paused) {
                want();
                pause();
            }
        },
        // A session seek asks before it starts its new run (and before a
        // refused one restarts the old): held -- and the answer starts it.
        holds() {
            if (!active) return false;
            want();
            return true;
        },
        // Returns whether the answer started playback.
        release({ play = false } = {}) {
            if (!active) return false;
            const go = resume || play;
            drop();
            if (go && video.paused && typeof video.play === 'function') {
                const r = video.play();
                if (r && typeof r.catch === 'function') r.catch(() => {});
            }
            return go;
        },
        dispose: drop,
        isActive: () => active,
        // The popup is holding playback that would otherwise run.
        held: () => active && resume,
    };
}
