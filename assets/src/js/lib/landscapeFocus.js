// A phone on its side: the film takes the window (owner, 2026-10-15).
//
// Measured on stage 2026-10-15 (844x390 and five other landscape phones):
// the navbar (72 px) and the sticky status bar (67 px, 156 with the plan
// box) covered 32-41% of the window -- 53-82% with the box -- over an
// inline film that was itself taller than the window, so its controls and
// the fullscreen button were under the fold on every one of them. Real
// fullscreen is no answer there: an iPhone has none for an element (the
// button opens the native video player, without our pill, lock or
// dialogs), and a turn of the phone is not a gesture a browser will enter
// fullscreen on.
//
// So in landscape on a touch phone the player's stage is the window's size,
// in the page's flow (player.css, FOCUS_MEDIA): its size never depends on
// the scroll, so nothing jumps under the finger. What this module adds is
// "focus": while the stage covers the window, html.wt-focus slides the
// navbar away and keeps the status bar down (stickyStatus.js), and the
// stage's black runs to the window's edges. Scrolling on is what leaves
// it -- the film scrolls up with the page and the navbar comes back.
//
//   - in at 90% of the stage on screen, out under 70%: one threshold would
//     flicker on its edge;
//   - turned into landscape with the player on screen (half of it or more)
//     -> the stage to the window's top, at once: press Watch upright, turn
//     the phone when the film is up, and it is the whole screen. Turned
//     back -> focus off and the player under what is fixed at the top, as
//     reveal() would put it;
//   - the player shown while the phone is already on its side: reveal()
//     puts the stage at the top (stickyStatus.js);
//   - the viewer scrolled in by hand: once the scroll comes to rest and no
//     finger is down, the last few px to the top -- once per entry, so a
//     small scroll in focus is not pulled back.
//
// Counted in Umami: `player-focus` {via, h} on the way in, and
// `player-focus-exit` {via, exit, sec} on the way out -- how many get here,
// how (rotate, reveal, scroll), and for how long.
import { FOCUS_MEDIA, stickyBottom } from './stickyStatus';

const ON = 0.9;
const OFF = 0.7;
// How much of the player has to be on screen for a turn of the phone to
// bring it to the top: it was what the viewer was looking at.
const LOOKING = 0.5;
const REST_MS = 150;

export function initLandscapeFocus(doc = document) {
    const w = doc.defaultView;
    if (typeof w.matchMedia !== 'function' || typeof w.IntersectionObserver !== 'function') return null;
    // The embed fills its iframe already (player.css data-fixed-height).
    if (doc.body.hasAttribute('data-iframe-height')) return null;
    const mq = w.matchMedia(FOCUS_MEDIA);
    const html = doc.documentElement;
    let stage = null;
    let ratio = 0;
    let via = '';
    let since = 0;
    // The one-shot pull to the top after a scroll in (rest() below).
    let settleArmed = false;
    let touching = false;
    let restTimer = null;
    const isOn = () => html.classList.contains('wt-focus');
    // A video player, or the moment between two of them (next episode, a
    // stream restart: player-stage.js holds the stage's height meanwhile) --
    // focus does not blink between episodes.
    const film = () => !!(stage && stage.isConnected
        && (stage.querySelector('.wt-player--video') || stage.classList.contains('wt-player-stage--switching')));
    const track = (name, props) => { if (w.umami) w.umami.track(name, props); };

    const set = (on, exit) => {
        if (on === isOn()) return;
        html.classList.toggle('wt-focus', on);
        doc.dispatchEvent(new w.CustomEvent('landscape-focus', { detail: { on } }));
        if (on) {
            via = (stage && stage.dataset.focusVia) || 'scroll';
            if (stage) delete stage.dataset.focusVia;
            since = Date.now();
            track('player-focus', { via, h: Math.round(w.innerHeight / 10) * 10 });
            if (via === 'scroll') settleArmed = true;
        } else {
            settleArmed = false;
            track('player-focus-exit', { via, exit, sec: Math.round((Date.now() - since) / 1000) });
        }
    };
    const update = (exit) => set(mq.matches && film() && ratio >= (isOn() ? OFF : ON), exit);

    const io = new w.IntersectionObserver((entries) => {
        for (const e of entries) if (e.target === stage) ratio = e.isIntersecting ? e.intersectionRatio : 0;
        update('scroll');
    }, { threshold: [0, LOOKING, OFF, ON, 1] });

    // The stage comes with the player and goes with a file picked
    // elsewhere (a #content swap): looked up again whenever either may have
    // happened. Hidden until the player is ready (app/action.js), so the
    // observer's first word is "not on screen" and the next one is real.
    const watch = () => {
        const s = doc.querySelector('.wt-player-stage');
        if (s !== stage) {
            if (stage) io.unobserve(stage);
            stage = s;
            ratio = 0;
            if (stage) io.observe(stage);
        }
        update('gone');
    };
    const WATCHED = ['player_ready', 'player_show', 'player_replaced', 'async'];
    for (const ev of WATCHED) w.addEventListener(ev, watch);

    // Instant: a turn of the phone is a jump already, and a slide after it
    // reads as lag. Once on the next frame and once more on the resize that
    // follows: iOS reports the new window a few frames after the turn.
    let unalign = null;
    const align = (offset) => {
        if (unalign) unalign();
        const go = () => {
            if (!stage || !stage.isConnected) return;
            const dy = offset(stage.getBoundingClientRect().top);
            if (Math.abs(dy) >= 1) w.scrollTo({ top: w.scrollY + dy, behavior: 'instant' });
        };
        const once = () => { if (unalign) unalign(); go(); };
        const timer = w.setTimeout(() => unalign && unalign(), 1000);
        unalign = () => { w.removeEventListener('resize', once); w.clearTimeout(timer); unalign = null; };
        w.addEventListener('resize', once);
        w.requestAnimationFrame(go);
    };
    // The change event comes before the observer has seen the new layout:
    // `ratio` is still what the viewer had on screen before the turn.
    const onTurn = () => {
        const looking = film() && ratio >= LOOKING;
        const was = isOn();
        // Before update(): a player wholly in view upright reads 1 still, and
        // focus comes in on this very call -- by the turn.
        if (looking && mq.matches) stage.dataset.focusVia = 'rotate';
        update('rotate');
        if (!looking && !was) return;
        if (mq.matches) align((top) => top);
        else align((top) => top - (stickyBottom(doc) + 12));
    };
    mq.addEventListener('change', onTurn);

    // In by the viewer's own scroll: the rest of the way once it settles.
    const rest = () => {
        restTimer = null;
        if (!settleArmed || touching || !isOn() || !film()) return;
        settleArmed = false;
        const top = stage.getBoundingClientRect().top;
        if (Math.abs(top) < 1) return;
        const reduce = w.matchMedia('(prefers-reduced-motion: reduce)').matches;
        w.scrollTo({ top: w.scrollY + top, behavior: reduce ? 'instant' : 'smooth' });
    };
    const onScroll = () => {
        if (!settleArmed) return;
        w.clearTimeout(restTimer);
        restTimer = w.setTimeout(rest, REST_MS);
    };
    const onTouch = (e) => {
        touching = e.type === 'touchstart';
        if (!touching) onScroll();
    };
    const TOUCH = ['touchstart', 'touchend', 'touchcancel'];
    w.addEventListener('scroll', onScroll, { passive: true });
    for (const ev of TOUCH) w.addEventListener(ev, onTouch, { passive: true });

    watch();
    return () => {
        if (unalign) unalign();
        w.clearTimeout(restTimer);
        io.disconnect();
        mq.removeEventListener('change', onTurn);
        for (const ev of WATCHED) w.removeEventListener(ev, watch);
        w.removeEventListener('scroll', onScroll);
        for (const ev of TOUCH) w.removeEventListener(ev, onTouch);
        if (isOn()) set(false, 'gone');
    };
}
