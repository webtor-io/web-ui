import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const {
    playerState, playerStreaming, createPlayerActivity, STALL_WINDOW_MS, ACTIVE_WINDOW_MS, STALL_MIN_MS, BUFFER_IDLE_MS,
    RESUME_MIN_S,
} = await import('./playerActivity.js');

const media = (o = {}) => ({ paused: false, ended: false, ...o });

test('the verdict: no player, playing, buffering for a minute after a stall', () => {
    assert.equal(playerState([], {}, 0), 'none');
    assert.equal(playerState([media({ ended: true })], {}, 0), 'none', 'an ended film is not playing');
    assert.equal(playerState([media()], {}, 1000), 'playing');
    const marks = { lastStallAt: 10_000, lastActiveAt: 10_000 };
    assert.equal(playerState([media()], marks, 10_001), 'buffering');
    assert.equal(playerState([media()], marks, 10_000 + STALL_WINDOW_MS - 1), 'buffering', 'the whole minute');
    assert.equal(playerState([media()], marks, 10_000 + STALL_WINDOW_MS), 'playing');
    // A stall under way counts once it has lasted.
    assert.equal(playerState([media()], { stallingSince: 5_000 }, 5_000 + STALL_MIN_MS - 1), 'playing', 'a hiccup');
    assert.equal(playerState([media()], { stallingSince: 5_000 }, 5_000 + STALL_MIN_MS), 'buffering');
    // Paused: still the stream for a minute after it last played...
    const paused = [media({ paused: true })];
    assert.equal(playerState(paused, { lastActiveAt: 5_000 }, 5_000 + ACTIVE_WINDOW_MS - 1), 'playing');
    assert.equal(playerState(paused, { lastActiveAt: 5_000 }, 5_000 + ACTIVE_WINDOW_MS), 'none', 'left alone: a download now');
    // ...and for as long as it keeps filling its buffer.
    const later = 5_000 + ACTIVE_WINDOW_MS + 20_000;
    assert.equal(playerState(paused, { lastActiveAt: 5_000, lastBufferAt: later - 1_000 }, later), 'playing', 'still buffering ahead');
    assert.equal(playerState(paused, { lastActiveAt: 5_000, lastBufferAt: later - BUFFER_IDLE_MS }, later), 'none');
});

function setup(attrs = 'data-status-stall-sub="Без подписки — до 5 Мбит/с, а файлу нужно 8 Мбит/с"') {
    const dom = new JSDOM(`<!doctype html><body><div id="file"><video ${attrs}></video></div></body>`);
    const doc = dom.window.document;
    const video = doc.querySelector('video');
    // jsdom's media element never plays: its state is set by hand.
    const set = (props) => {
        for (const [k, v] of Object.entries(props)) Object.defineProperty(video, k, { configurable: true, get: () => v });
    };
    set({ paused: true, ended: false, seeking: false, readyState: 0, currentTime: 0 });
    let clock = 1_000_000;
    const changes = [];
    const activity = createPlayerActivity(doc, { now: () => clock, onChange: (s) => changes.push(s) });
    // Media events do not bubble -- exactly like the real ones.
    const fire = (name) => video.dispatchEvent(new dom.window.Event(name, { bubbles: false }));
    const buffered = (end) => set({ buffered: { length: 1, end: () => end } });
    return { doc, video, set, fire, buffered, activity, changes, tick: (ms) => { clock += ms; } };
}

// A source that has played and then waits: a stall once it lasts.
function playing(p) {
    p.fire('loadstart');
    p.set({ paused: false, readyState: 4 });
    p.fire('playing');
}

test('heard from the document without the player: playing, then a real stall', () => {
    const p = setup();
    assert.equal(p.activity.state(), 'none');
    playing(p);
    assert.equal(p.activity.state(), 'playing');
    p.set({ readyState: 2 });
    p.fire('waiting');
    assert.equal(p.activity.state(), 'playing', 'not yet: a hiccup sells nothing');
    p.tick(STALL_MIN_MS);
    assert.equal(p.activity.state(), 'buffering', 'still waiting: a stall');
    p.set({ readyState: 4 });
    p.fire('playing');
    assert.equal(p.activity.state(), 'buffering', 'the minute after it');
    p.tick(STALL_WINDOW_MS);
    assert.equal(p.activity.state(), 'playing', 'a minute later, if nothing stalled again');
    assert.deepEqual(p.changes, ['playing', 'buffering'], 'told on an event that changes it, once each');
    p.activity.stop();
});

// Chrome's MSE element running dry: `waiting`, then one more periodic
// `timeupdate` ~250 ms later with the clock where it stopped, then nothing
// until the data comes. That tick is the stall reported, not playback: it
// closed every stall at ~255 ms and nothing read as buffering at the cap
// (playerActivity.trace.test.js replays the recording).
test('a timeupdate that did not move the clock does not end the stall; one that did, does', () => {
    const p = setup();
    playing(p);
    p.set({ currentTime: 75.861, readyState: 2 });
    p.fire('waiting');
    p.tick(255);
    p.fire('timeupdate');
    p.tick(STALL_MIN_MS);
    assert.equal(p.activity.state(), 'buffering', 'still frozen at 75.861: a stall');
    // A tick that moved the clock a little less than RESUME_MIN_S: not yet.
    p.set({ currentTime: 75.861 + RESUME_MIN_S / 2 });
    p.fire('timeupdate');
    assert.equal(p.activity.state(), 'buffering', 'the clock crept, the stall stands');
    // Resumed without a `playing` (a played tick, ~0.25 s at 1x, is enough).
    p.set({ currentTime: 76.111, readyState: 4 });
    p.fire('timeupdate');
    p.tick(STALL_WINDOW_MS);
    assert.equal(p.activity.state(), 'playing', 'resumed; a minute later nothing is left of it');
    // A hiccup: the tick, then playback within STALL_MIN_MS.
    p.set({ currentTime: 90, readyState: 2 });
    p.fire('waiting');
    p.tick(250);
    p.fire('timeupdate');
    p.tick(STALL_MIN_MS - 400);
    p.set({ currentTime: 90.3, readyState: 4 });
    p.fire('timeupdate');
    p.tick(1000);
    assert.equal(p.activity.state(), 'playing', 'a wait under STALL_MIN_MS is not a stall');
    p.activity.stop();
});

test('a wait that ends within STALL_MIN_MS is not a stall', () => {
    const p = setup();
    playing(p);
    p.set({ readyState: 2 });
    p.fire('waiting');
    p.tick(STALL_MIN_MS - 100);
    p.set({ readyState: 4 });
    p.fire('playing');
    p.tick(100);
    assert.equal(p.activity.state(), 'playing');
    p.activity.stop();
});

// The session seek reloads the source (player/session-seek.js): the element
// is emptied, play() waits for its first data with `seeking` false. The
// start of a source is not the cap -- and neither is the very first play().
test('a (re)started source waits before it plays: not a stall', () => {
    const p = setup();
    // The first play of the stream.
    p.fire('loadstart');
    p.set({ paused: false, readyState: 0 });
    p.fire('waiting');
    p.tick(STALL_MIN_MS * 4);
    assert.equal(p.activity.state(), 'playing', 'the first frame is not a stall');
    p.set({ readyState: 4 });
    p.fire('playing');
    // A session seek: pause, the source reloaded, play().
    p.set({ paused: true });
    p.fire('pause');
    p.set({ readyState: 0 });
    p.fire('emptied');
    p.fire('loadstart');
    p.set({ paused: false });
    p.fire('waiting');
    p.tick(STALL_MIN_MS * 4);
    assert.equal(p.activity.state(), 'playing', 'the jump, not the cap');
    p.set({ readyState: 4 });
    p.fire('playing');
    for (let i = 0; i < 40; i++) {
        p.tick(1000);
        p.fire('timeupdate');
    }
    assert.equal(p.activity.state(), 'playing', 'smooth after the seek: no stall window');
    // Once it has played, a wait is a stall again.
    p.set({ readyState: 2 });
    p.fire('waiting');
    p.tick(STALL_MIN_MS);
    assert.equal(p.activity.state(), 'buffering');
    p.activity.stop();
});

test('a seek is not a stall, and neither is `stalled` over a healthy buffer', () => {
    const p = setup();
    playing(p);
    p.set({ seeking: true, readyState: 1 });
    p.fire('waiting');
    p.tick(STALL_MIN_MS);
    assert.equal(p.activity.state(), 'playing', 'the jump, not the cap');
    p.set({ seeking: false, readyState: 4 });
    p.fire('stalled');
    p.tick(STALL_MIN_MS);
    assert.equal(p.activity.state(), 'playing', 'MSE fires `stalled` while it plays on');
    p.set({ readyState: 2 });
    p.fire('stalled');
    p.tick(STALL_MIN_MS);
    assert.equal(p.activity.state(), 'buffering', 'short of data: a stall');
    p.activity.stop();
});

// hls.js keeps fetching far ahead of a paused video (50 MB, over a minute at
// a 5M cap): those bytes are the stream's, not a download's.
test('paused and still buffering ahead: the stream, not a download', () => {
    const p = setup();
    playing(p);
    p.set({ paused: true });
    p.fire('pause');
    p.buffered(30);
    let end = 30;
    for (let s = 0; s < ACTIVE_WINDOW_MS / 1000 + 20; s++) {
        p.tick(1000);
        end += 2;
        p.buffered(end);
        assert.equal(p.activity.state(), 'playing', `paused +${s + 1}s, buffer growing`);
    }
    // The buffer is full: nothing moves any more.
    p.tick(BUFFER_IDLE_MS);
    assert.equal(p.activity.state(), 'none');
    p.activity.stop();
});

// A pause with the buffer full: nothing is fetched any more, and nothing
// keeps the viewer on the status chain -- at once, not BUFFER_IDLE_MS later,
// and at every pause, not only the first. The paused buffer is compared
// with the buffer as it was when the player paused; it used to be compared
// with the buffer before the player ever played (or at its previous pause),
// and every pause read as "still buffering" for ten seconds.
test('paused with a full buffer: not streaming at once, pause after pause', () => {
    const p = setup();
    p.buffered(0);
    p.fire('loadstart');
    p.set({ paused: false, readyState: 4 });
    p.buffered(60);
    p.fire('playing');
    p.tick(250);
    p.fire('timeupdate');
    p.set({ paused: true });
    p.fire('pause');
    assert.equal(p.activity.streaming(), false, 'paused, the buffer full: at once');
    p.tick(5000);
    assert.equal(p.activity.streaming(), false);
    // Ten minutes more, the buffer running ahead all along; paused again.
    p.set({ paused: false });
    p.fire('playing');
    for (let s = 1; s <= 600; s++) {
        p.tick(1000);
        p.buffered(60 + s);
        p.fire('timeupdate');
    }
    p.set({ paused: true });
    p.fire('pause');
    assert.equal(p.activity.streaming(), false, 'the second pause: at once too');
    // Fetching ahead after the pause: streaming from the first growth.
    p.tick(1000);
    p.buffered(700);
    assert.equal(p.activity.streaming(), true, 'the buffer grows while paused');
    p.tick(BUFFER_IDLE_MS);
    assert.equal(p.activity.streaming(), false, 'full again');
    p.activity.stop();
});

// Whether the page's player is streaming now -- the transfer status keeps
// the viewer on its chain while it is (lib/transferStatus.js playing), since
// HLS closes its request between two segments and the proxy honestly counts
// none: a player that is not paused (playing, or waiting for data), or one
// paused and still filling its buffer. No minute after a pause: that minute
// is the plan box's, which asks what a cap means to them, not whether they
// are there.
test('streaming: playing or waiting now, or paused and still buffering -- no minute after a pause', () => {
    assert.equal(playerStreaming([], {}, 0), false);
    assert.equal(playerStreaming([media()], {}, 0), true);
    assert.equal(playerStreaming([media({ ended: true })], {}, 0), false, 'an ended film');
    const paused = [media({ paused: true })];
    assert.equal(playerStreaming(paused, { lastActiveAt: 5_000 }, 5_001), false, 'paused and full: not streaming');
    assert.equal(playerState(paused, { lastActiveAt: 5_000 }, 5_001), 'playing', 'while the plan box keeps its minute');
    assert.equal(playerStreaming(paused, { lastBufferAt: 5_000 }, 5_000 + BUFFER_IDLE_MS - 1), true, 'paused, buffering ahead');
    assert.equal(playerStreaming(paused, { lastBufferAt: 5_000 }, 5_000 + BUFFER_IDLE_MS), false);
});

// The grace popup holds the film until the viewer answers it
// (player/grace-hold.js marks the element data-grace-cta-hold): the page's
// pause, not theirs. They are reading the popup, hls.js keeps filling the
// buffer at the cap, and the film goes on with the answer -- read as it read
// while the film played on under the popup: on the chain, no minute running
// out, and nothing redrawn at the pause.
test('the grace popup\'s pause is the page\'s: still the stream, no minute running out', () => {
    const held = [media({ paused: true, dataset: { graceCtaHold: '' } })];
    assert.equal(playerStreaming(held, {}, 0), true);
    assert.equal(playerState(held, {}, 0), 'playing', 'no lastActiveAt needed');
    assert.equal(playerState([media({ paused: true, dataset: {} })], {}, 0), 'none', 'the same pause, the viewer’s');

    const p = setup('data-grace-duration-sec="1200" data-status-over-cap');
    p.buffered(0);
    playing(p);
    p.buffered(60);
    p.tick(250);
    p.fire('timeupdate');
    const told = p.changes.length;
    p.video.dataset.graceCtaHold = '';
    p.set({ paused: true });
    p.fire('pause');
    assert.equal(p.activity.streaming(), true, 'the viewer stays on the chain, the buffer full or not');
    assert.equal(p.activity.state(), 'playing');
    assert.equal(p.changes.length, told, 'nothing to redraw at the popup’s pause');
    p.tick(ACTIVE_WINDOW_MS + BUFFER_IDLE_MS);
    assert.equal(p.activity.streaming(), true, 'a popup left unanswered');
    assert.equal(p.activity.state(), 'playing');
    // The mark goes with the answer (the film plays again) or with the
    // player: a pause after it is the viewer's, by the ordinary rules.
    delete p.video.dataset.graceCtaHold;
    assert.equal(p.activity.streaming(), false, 'their pause, the buffer full: at once');
    assert.equal(p.activity.state(), 'none', 'and the minute is long gone');
    p.activity.stop();
});

test('streaming follows the player on its events, and the view is told', () => {
    const p = setup();
    assert.equal(p.activity.streaming(), false, 'no play yet');
    playing(p);
    assert.equal(p.activity.streaming(), true);
    const told = p.changes.length;
    p.set({ paused: true });
    p.fire('pause');
    assert.equal(p.activity.streaming(), false, 'paused: at once');
    assert.equal(p.activity.state(), 'playing', 'the verdict keeps its minute');
    assert.equal(p.changes.length, told + 1, 'told, though the verdict did not change');
    p.set({ paused: false });
    p.fire('playing');
    assert.equal(p.activity.streaming(), true);
    assert.equal(p.changes.length, told + 2);
    p.activity.stop();
});

test('the stalled player\'s own line, for the plan box', () => {
    const p = setup();
    assert.equal(p.activity.stallSub(), 'Без подписки — до 5 Мбит/с, а файлу нужно 8 Мбит/с');
    assert.equal(p.activity.fitsCap(), false);
    p.video.remove();
    assert.equal(p.activity.stallSub(), '');
    p.activity.stop();
});

test('a file under the cap: the player says so', () => {
    const p = setup('data-status-stall-sub="Без подписки — до 5 Мбит/с" data-status-fits-cap');
    assert.equal(p.activity.fitsCap(), true);
    assert.equal(p.activity.overCap(), false);
    p.activity.stop();
});

test('a file over the cap: the player says so', () => {
    const p = setup('data-status-stall-sub="Без подписки — до 5 Мбит/с, а файлу нужно 8 Мбит/с" data-status-over-cap');
    assert.equal(p.activity.overCap(), true);
    assert.equal(p.activity.fitsCap(), false);
    p.activity.stop();
    const q = setup('data-status-stall-sub="Без подписки — до 5 Мбит/с"');
    assert.equal(q.activity.overCap(), false, 'bitrate unknown: neither');
    assert.equal(q.activity.fitsCap(), false);
    q.activity.stop();
});

// The page knows where its player is in the free grace window
// (data-grace-duration-sec, set only where grace applies): by its movie
// time, currentTime plus the offset its transcoder session started at
// (data-run-offset, Player.jsx) -- after a session seek currentTime counts
// from the seek point.
test('inside the grace window: by the player\'s own movie time', () => {
    const p = setup('data-grace-duration-sec="1200"');
    assert.equal(p.activity.inGrace(), true, 'at the start');
    p.set({ currentTime: 1199 });
    assert.equal(p.activity.inGrace(), true);
    p.set({ currentTime: 1200 });
    assert.equal(p.activity.inGrace(), false, 'past the window');
    // A session seek to 24:30: the element's clock starts over.
    p.video.dataset.runOffset = '1470';
    p.set({ currentTime: 6 });
    assert.equal(p.activity.inGrace(), false, '24:36 of the film, 0:06 of the element');
    // And back to 10:00.
    p.video.dataset.runOffset = '600';
    assert.equal(p.activity.inGrace(), true, '10:06');
    p.video.dataset.runOffset = '0';
    p.set({ currentTime: 30, ended: true });
    assert.equal(p.activity.inGrace(), false, 'ended');
    p.activity.stop();
    const q = setup();
    assert.equal(q.activity.inGrace(), false, 'no grace for this viewer');
    q.activity.stop();
});

// A player removed mid-stall: its `pause`/`emptied` fire on a detached node
// and never pass through the document -- picking another file (#content
// swapped before the player's async teardown) or "Next" on a prewarmed card
// (pause() and remove() in one task). The next file, playing smoothly, must
// not inherit the stall for good: it counts as far as it was seen, the
// minute after it like after a pause mid-stall, and then it is over.
function swapPlayer(p) {
    p.video.remove();
    const next = p.doc.createElement('video');
    p.doc.getElementById('file').appendChild(next);
    const set = (props) => {
        for (const [k, v] of Object.entries(props)) Object.defineProperty(next, k, { configurable: true, get: () => v });
    };
    set({ paused: true, ended: false, seeking: false, readyState: 0, currentTime: 0 });
    const fire = (name) => next.dispatchEvent(new p.doc.defaultView.Event(name, { bubbles: false }));
    fire('loadstart');
    set({ paused: false, readyState: 4 });
    fire('playing');
    return { el: next, set, fire };
}

test('a player removed mid-stall does not leave the next one buffering', () => {
    const p = setup();
    playing(p);
    p.set({ currentTime: 75.861, readyState: 2 });
    p.fire('waiting');
    p.tick(255);
    p.fire('timeupdate');
    p.tick(STALL_MIN_MS + 1000);
    assert.equal(p.activity.state(), 'buffering', 'a real stall, seen lasting');
    const next = swapPlayer(p);
    p.tick(1000);
    assert.equal(p.activity.state(), 'buffering', 'the minute after the stall, as after a pause mid-stall');
    for (let s = 0; s < STALL_WINDOW_MS / 1000; s++) {
        p.tick(1000);
        next.set({ currentTime: s + 1 });
        next.fire('timeupdate');
    }
    assert.equal(p.activity.state(), 'playing', 'the next file plays: the stall is over');
    p.tick(5 * 60 * 1000);
    assert.equal(p.activity.state(), 'playing');
    p.activity.stop();
});

test('a wait cut short by the player leaving is not a stall', () => {
    const p = setup();
    playing(p);
    p.set({ readyState: 2 });
    p.fire('waiting');
    p.tick(STALL_MIN_MS / 3);
    swapPlayer(p);
    p.tick(STALL_MIN_MS * 4);
    assert.equal(p.activity.state(), 'playing', 'half a second of waiting, then another file');
    p.activity.stop();
});

// The grace popup is put up by the player (Player.jsx) from a render
// effect, a frame or more after the element crosses the window, and not at
// all while the tab is hidden. Between the crossing and the popup the offer
// is on its way -- one signal with inGrace, so the status's plan box does
// not come up for those frames and then fold into a line.
test('the grace popup on its way: from the crossing until the player marks it up', () => {
    const p = setup('data-grace-duration-sec="1200"');
    const cta = p.doc.createElement('div');
    cta.setAttribute('data-upsell-surface', 'grace');
    cta.className = 'hidden';
    p.doc.body.appendChild(cta);
    p.set({ currentTime: 1199.98 });
    assert.equal(p.activity.inGrace(), true);
    assert.equal(p.activity.graceOfferDue(), false, 'inside the window: nothing due');
    p.set({ currentTime: 1200.01 });
    assert.equal(p.activity.inGrace(), false);
    assert.equal(p.activity.graceOfferDue(), true, 'crossed, the popup not up yet');
    p.video.dataset.graceCtaShown = '';
    assert.equal(p.activity.graceOfferDue(), false, 'up (or closed since): the popup speaks for itself');
    // A session seek past the window before the popup: movie time counts.
    delete p.video.dataset.graceCtaShown;
    p.video.dataset.runOffset = '1470';
    p.set({ currentTime: 6 });
    assert.equal(p.activity.graceOfferDue(), true, '24:36 of the film');
    p.video.dataset.runOffset = '0';
    assert.equal(p.activity.graceOfferDue(), false, 'back inside');
    // No popup on the page (the audio page has none): nothing on its way.
    p.video.dataset.runOffset = '1470';
    cta.remove();
    assert.equal(p.activity.graceOfferDue(), false);
    p.activity.stop();
    const q = setup();
    q.set({ currentTime: 5000 });
    assert.equal(q.activity.graceOfferDue(), false, 'no grace for this viewer');
    q.activity.stop();
});

// The owner's flow (2026-09-26): the grace popup tells the viewer the cap is
// coming; they answer "continue at N Mbps" (or close it), and the player marks
// the element (data-grace-cta-answered). From then on the next offer waits
// for them to hit the cap -- the player's first real stall. A hiccup, a
// session seek or the popup merely shown is not it; the next file is a new
// element and starts without the mark.
test('the grace popup answered: until the player\'s first real stall', () => {
    for (const answer of ['continue', 'dismiss']) {
        const p = setup('data-grace-duration-sec="1200" data-status-over-cap');
        playing(p);
        p.set({ currentTime: 1201 });
        assert.equal(p.activity.offerAnswered(), false, 'no answer yet');
        p.video.dataset.graceCtaShown = '';
        assert.equal(p.activity.offerAnswered(), false, 'shown is not answered');
        p.video.dataset.graceCtaAnswered = answer;
        assert.equal(p.activity.offerAnswered(), true, answer);
        // A hiccup: not the cap.
        p.set({ readyState: 2 });
        p.fire('waiting');
        p.tick(STALL_MIN_MS - 1);
        p.set({ readyState: 4 });
        p.fire('playing');
        assert.equal(p.activity.offerAnswered(), true, 'a hiccup');
        // A session seek restarts the source, not the answer.
        p.fire('loadstart');
        p.fire('playing');
        p.tick(30_000);
        assert.equal(p.activity.offerAnswered(), true, 'a session seek');
        // A real stall: the cap they were told of.
        p.set({ readyState: 2 });
        p.fire('waiting');
        p.tick(STALL_MIN_MS);
        assert.equal(p.activity.state(), 'buffering');
        assert.equal(p.activity.offerAnswered(), false, 'hit the cap, still frozen');
        p.set({ readyState: 4 });
        p.fire('playing');
        p.tick(STALL_WINDOW_MS);
        assert.equal(p.activity.state(), 'playing');
        assert.equal(p.activity.offerAnswered(), false, 'once hit, the answer is spent for this element');
        // The status view re-inits (a refused stream, status.js renew) and
        // listens anew: the spent answer is the element's, not the old
        // listener's.
        const again = createPlayerActivity(p.doc, { now: () => 2_000_000 });
        assert.equal(again.offerAnswered(), false, 'still spent after a re-init');
        again.stop();
        // The next file: a new element, its own popup and answer.
        const next = swapPlayer(p);
        assert.equal(p.activity.offerAnswered(), false, 'no answer on the new element');
        next.el.dataset.graceCtaAnswered = answer;
        assert.equal(p.activity.offerAnswered(), true, 'answered anew');
        p.activity.stop();
    }
    const q = setup();
    playing(q);
    assert.equal(q.activity.offerAnswered(), false, 'no grace popup for this viewer');
    q.activity.stop();
});

// "Watch as is" on the slow-download modal before playback is the same kind
// of answer (owner, 2026-09-26): the stream job renders the force-slow run's
// player with data-offer-answered, and the next offer waits for the first
// real stall, as after the grace popup.
test('"watch as is" on the slow-download modal: answered until the first real stall', () => {
    const p = setup('data-status-over-cap data-offer-answered="continue-slow"');
    playing(p);
    assert.equal(p.activity.offerAnswered(), true, 'answered before playback');
    p.set({ readyState: 2 });
    p.fire('waiting');
    p.tick(STALL_MIN_MS - 1);
    p.set({ readyState: 4 });
    p.fire('playing');
    assert.equal(p.activity.offerAnswered(), true, 'a hiccup');
    p.set({ readyState: 2 });
    p.fire('waiting');
    p.tick(STALL_MIN_MS);
    assert.equal(p.activity.state(), 'buffering');
    assert.equal(p.activity.offerAnswered(), false, 'hit the cap');
    p.activity.stop();
});

// A stall that ended before the answer was the popup's to speak of: it does
// not spend the answer, though the verdict stays 'buffering' for its minute.
// One still under way when the viewer answers is the cap, now: the video is
// frozen at it.
test('the grace popup answered: a stall before the answer does not count, one under way does', () => {
    const p = setup('data-grace-duration-sec="1200" data-status-over-cap');
    playing(p);
    p.set({ readyState: 2, currentTime: 1300 });
    p.fire('waiting');
    p.tick(STALL_MIN_MS * 2);
    p.set({ readyState: 4, currentTime: 1300.3 });
    p.fire('playing');
    assert.equal(p.activity.state(), 'buffering', 'the minute after it');
    p.video.dataset.graceCtaAnswered = 'continue';
    assert.equal(p.activity.offerAnswered(), true, 'ended before the answer');
    p.activity.stop();

    const q = setup('data-grace-duration-sec="1200" data-status-over-cap');
    playing(q);
    q.set({ readyState: 2, currentTime: 1300 });
    q.fire('waiting');
    q.tick(STALL_MIN_MS * 2);
    q.video.dataset.graceCtaAnswered = 'continue';
    assert.equal(q.activity.offerAnswered(), false, 'frozen at the cap as they answer');
    q.activity.stop();
});

test('stop() stops listening', () => {
    const p = setup();
    p.activity.stop();
    playing(p);
    p.set({ readyState: 1 });
    p.fire('waiting');
    assert.deepEqual(p.changes, []);
});
