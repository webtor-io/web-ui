// The picker wiring, against the markup Go actually emits.
//
// Player.jsx was the one file `node --test` could not see: it is JSX, and
// the suite had nothing to parse it with. Everything in here used to be a
// by-hand step in the stage checklist. Three pieces make it runnable:
//
//   - assets/src/js/test/jsx-hooks.mjs transpiles .jsx with the repo's own
//     babel config (wired into `npm test` as --import);
//   - jsdom gives a real DOM, so `closest`, delegation, `hidden` and
//     attribute reads behave the way they do in a browser rather than the
//     way a stand-in was written to;
//   - __fixtures__/subtitles-dialog.html is the dialog rendered by
//     services/template/subtitles_dialog_fixture_test.go. A hand-written
//     fixture drifts from the template in silence, and a wiring test that
//     passes against markup the server stopped producing is worse than no
//     test at all. That Go test fails when the two disagree and prints how
//     to regenerate.
//
// What is NOT faked: the DOM, the delegation, track-picker.js, the pure
// rules. What is: `fetch` (every test asserts on the calls), `window.umami`
// (the events are the assertion), and `video.textTracks`, which jsdom does
// not implement — a small live view over the <track> elements, so
// activateSubtitle's mode writes are observable.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const DIALOG = readFileSync(path.join(HERE, '__fixtures__/subtitles-dialog.html'), 'utf8');

// One jsdom for the file, rebuilt per test. The globals have to exist
// before Player.jsx is imported: hls-manager.js reads navigator.userAgent
// at module scope, and lib/i18n.js reads __SUPPORTED_LOCALES__ (webpack's
// DefinePlugin supplies it in a build).
const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    url: 'https://webtor.io/res?file=movie.mkv',
    pretendToBeVisual: true,
});
globalThis.window = dom.window;
globalThis.document = dom.window.document;
// Node 21+ has its own read-only `navigator`, and hls-manager.js reads
// userAgent/platform off it at module scope. defineProperty, not
// assignment.
Object.defineProperty(globalThis, 'navigator', {
    configurable: true,
    value: dom.window.navigator,
});
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.Event = dom.window.Event;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
// preact's hook scheduler pairs the two, and jsdom only exposes them on its
// own window.
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.__SUPPORTED_LOCALES__ = ['en'];
// jsdom has no requestIdleCallback, and the codec-support report waits for
// one (2 s without it). A prompt stand-in keeps a report inside the test
// whose `playing` caused it, instead of landing in a later test's events.
dom.window.requestIdleCallback = (fn) => setTimeout(fn, 0);

// The one media API these tests need and jsdom does not implement, taught
// to the prototypes so every <track> in the document has it — including the
// ones activateSubtitle creates on first selection.
//
// Both halves matter and they are not the same object in the browser
// either: activateSubtitle writes modes through video.textTracks, while
// dropDeletedTracks reads el.track.mode off the element. They share one
// per-element store here, so a mode written through either is seen through
// both. `default` seeds "showing", as a browser does.
const trackModes = new WeakMap();
// Cues, per element, for the tests that are about cue times (the session
// offset). Empty unless a test puts some in; `addCue` is what the reload's
// cue restore calls.
const trackCues = new WeakMap();
const cuesOf = (el) => {
    if (!trackCues.has(el)) trackCues.set(el, []);
    return trackCues.get(el);
};
Object.defineProperty(dom.window.HTMLTrackElement.prototype, 'track', {
    configurable: true,
    get() {
        const el = this;
        return {
            get id() { return el.id; },
            get mode() {
                if (trackModes.has(el)) return trackModes.get(el);
                return el.hasAttribute('default') ? 'showing' : 'disabled';
            },
            set mode(v) { trackModes.set(el, v); },
            get cues() { return cuesOf(el); },
            addCue(c) { cuesOf(el).push(c); },
        };
    },
});
Object.defineProperty(dom.window.HTMLMediaElement.prototype, 'textTracks', {
    configurable: true,
    get() { return Array.from(this.querySelectorAll('track')).map((el) => el.track); },
});

// jsdom implements no playback: `paused` is a getter that is always true,
// and play()/pause() do nothing. Made settable here (defaulting to true,
// the value jsdom reports) so a test can put the element in the state a
// browser would be in when it fires `play` or `pause` — the events
// themselves are dispatched by hand for the same reason.
const pausedState = new WeakMap();
Object.defineProperty(dom.window.HTMLMediaElement.prototype, 'paused', {
    configurable: true,
    get() { return pausedState.has(this) ? pausedState.get(this) : true; },
    set(v) { pausedState.set(this, !!v); },
});

const {
    initPlayer,
    destroyPlayer,
    wireTrackHandlers,
    syncUploadMarks,
    markTrack,
    findSubtitleItem,
    PUT_RETRY_DELAY_MS,
    swapSubtitlesDialog,
} = await import('./Player.jsx');
// Imported the same way and for the same reason as Player.jsx: hls-manager
// reads navigator at module scope, so it cannot be a static import above
// the globals.
const { Hls, initDefaultTracks } = await import('./hls-manager.js');
const { createSessionSeeker } = await import('./session-seek.js');
const { createGraceHold } = await import('./grace-hold.js');
const { createPlayerActivity } = await import('../playerActivity.js');
const { catchUpTiming } = await import('./subtitle-catchup.js');

// ---- the harness ----------------------------------------------------

// mount builds the page around the fixture: the dialog as rendered, plus
// the <video> the player drives and the preloaded <track> elements the
// server would have written for the tracks marked Preload.
//
// `tracks` lists those preloads as [id, showing]. They matter to two
// paths — dropDeletedTracks (a delete has to take the orphan <track> with
// it) and syncUploadMarks (what is playing is read off them).
//
// `hlsTracks` lists the tracks hls.js makes from the transcoder's manifest,
// as [label, mode]. In a browser those are created with addTextTrack and
// have no element behind them; here they are <track>s without an id, which
// is exactly the property every caller keys on — an id means element-backed
// and ours, no id means hls.js's.
function mount({ tracks = [], hlsTracks = [], tag = 'video' } = {}) {
    document.body.innerHTML = `
        <div id="page">
            <${tag} class="player" data-resource-id="res" data-path="movie.mkv">
                ${tracks.map(([id, showing]) => `<track id="${id}" src="https://x.test/${id}.vtt" srclang="en" label="${id}" kind="subtitles"${showing ? ' default="default"' : ''}>`).join('')}
                ${hlsTracks.map(([label]) => `<track src="https://x.test/manifest.vtt" srclang="ru" label="${label}" kind="subtitles">`).join('')}
            </${tag}>
            ${DIALOG}
        </div>`;
    const container = document.getElementById('page');
    const video = container.querySelector(`${tag}.player`);
    const hlsTrackEls = Array.from(video.querySelectorAll('track')).filter((el) => !el.id);
    hlsTracks.forEach(([, mode], i) => { if (mode) hlsTrackEls[i].track.mode = mode; });
    // A fake left on window by an earlier test would be picked up by the
    // next mount's activateSubtitle. Cleared before prepare() installs one.
    window.hlsPlayer = null;

    const calls = [];
    let respond = () => ({ ok: true, status: 200, headers: new dom.window.Headers() });
    globalThis.fetch = (url, params) => {
        calls.push({ url, params, body: params && params.body ? JSON.parse(params.body) : null });
        return Promise.resolve(respond(url, params, calls.length));
    };
    window.fetch = globalThis.fetch;
    window._CSRF = 'csrf-token';

    const events = [];
    window.umami = { track: (name, data) => events.push({ name, data }) };

    // The hooks object the mounted component fills in. wireTrackHandlers
    // only calls through it, so a test that is not about the component's
    // own state records the calls instead.
    const hooks = { selected: [], audio: [] };
    hooks.onSubtitleSelect = (el) => hooks.selected.push(el.getAttribute('data-id'));
    hooks.onAudioSelect = (el) => hooks.audio.push(el.getAttribute('data-id'));

    const modal = container.querySelector('#subtitles');
    return {
        container, video, modal, calls, events, hooks,
        setResponse: (fn) => { respond = fn; },
        wire: () => wireTrackHandlers(container, hooks),
        chip: (id) => findSubtitleItem(modal, id),
        audioChip: (id) => modal.querySelector(`.audio[data-id="${id}"]`),
        lang: (code) => modal.querySelector(`.lang[data-lang="${code}"]`),
        puts: () => calls.filter((c) => String(c.url).startsWith('/stream-video/')),
        // The two renderers, as the code under test sees them.
        installHls: (init) => { window.hlsPlayer = makeHls({ video, ...init }); return window.hlsPlayer; },
        hlsTrack: (i) => hlsTrackEls[i].track,
        hlsModes: () => hlsTrackEls.map((el) => el.track.mode),
        mode: (id) => {
            const t = video.textTracks.find((tt) => tt.id === id);
            return t ? t.mode : null;
        },
    };
}

// makeHls is hls.js as this code touches it — and, in the two places where
// hls.js does something back, as hls.js *behaves*. Those two are not an
// implementation of the library, they are the contract this fix is defined
// against, and leaving them out is what let a stack-exhausting recursion
// through the first round of these tests:
//
//   - the `subtitleTrack` setter runs a toggleTrackModes pass (hls.js:28415)
//     — every labeled subtitle track that is not its own current one goes to
//     'disabled', ours included — and then triggers SUBTITLE_TRACK_SWITCH
//     *synchronously* (hls.js:28448, and `trigger` is eventemitter3).
//     setSubtitleTrack has no "already -1" exit, so a write of the value it
//     already holds does all of this too;
//   - the `subtitleDisplay` setter toggles modes only while a track is
//     selected (`if (this.trackId > -1)`, hls.js:28505).
//
// What is still driven by hand is hls.js's *asynchronous* half —
// onTextTracksChanged, which reacts to the browser's textTracks `change`
// event and is what a test that wants the latch simulates directly.
function makeHls({ video = null, subtitleTrack = -1, subtitleDisplay = true } = {}) {
    const listeners = new Map();
    const writes = [];
    let t = subtitleTrack;
    let d = subtitleDisplay;
    const add = (ev, fn) => listeners.set(ev, [...(listeners.get(ev) || []), fn]);
    const fire = (ev, data) => { for (const fn of [...(listeners.get(ev) || [])]) fn(ev, data); };
    // The manifest tracks, as this harness models them: the <track>s with
    // no id (see mount). Read live — a side-loaded track is created on
    // first selection, so the list changes under us.
    const trackEls = () => (video ? Array.from(video.querySelectorAll('track')) : []);
    const manifestEls = () => trackEls().filter((el) => !el.id);
    const toggleTrackModes = () => {
        const current = t >= 0 ? manifestEls()[t] : null;
        for (const el of trackEls()) {
            // Labeled only, like filterSubtitleTracks — every track in this
            // harness is labeled, as every track in the page is.
            if (el !== current && el.track.mode !== 'disabled') el.track.mode = 'disabled';
        }
        if (current) current.track.mode = d ? 'showing' : 'hidden';
    };
    return {
        writes,
        audioTrack: -1,
        get subtitleTrack() { return t; },
        set subtitleTrack(v) {
            t = v;
            writes.push(['subtitleTrack', v]);
            toggleTrackModes();
            fire(Hls.Events.SUBTITLE_TRACK_SWITCH, { id: v });
        },
        get subtitleDisplay() { return d; },
        set subtitleDisplay(v) {
            d = v;
            writes.push(['subtitleDisplay', v]);
            if (t > -1) toggleTrackModes();
        },
        on: add,
        once: (ev, fn) => {
            const wrap = (...args) => {
                listeners.set(ev, (listeners.get(ev) || []).filter((f) => f !== wrap));
                fn(...args);
            };
            wrap.__wrapped = fn;
            add(ev, wrap);
        },
        off: (ev, fn) => listeners.set(ev, (listeners.get(ev) || []).filter((f) => f !== fn && f.__wrapped !== fn)),
        emit: fire,
        listenerCount: (ev) => (listeners.get(ev) || []).length,
        stopLoad: () => {},
        loadSource: () => {},
        destroy: () => {},
    };
}

// A real click, so the delegation does the work: the listener is on the
// dialog, not on the chip.
const click = (el) => el.dispatchEvent(new dom.window.MouseEvent('click', { bubbles: true, cancelable: true }));
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));
// settle waits past an animation frame: preact's effect scheduler runs
// there, so anything a mount does asynchronously has happened by now.
const settle = () => new Promise((resolve) => setTimeout(resolve, 50));
const active = (el) => el.classList.contains('track-chip-active');

// ---- selecting a track ----------------------------------------------

test('a subtitle click marks the chip and PUTs the choice', async () => {
    const p = mount();
    p.wire();
    const en = p.chip('os-os-en');
    assert.ok(en, 'the fixture must carry an OpenSubtitles English track');

    click(en);
    await flush();

    assert.ok(active(en), 'the clicked chip takes the fill');
    assert.equal(en.getAttribute('aria-checked'), 'true');
    assert.equal(en.getAttribute('data-default'), 'true');
    // Exactly one per group: the marker moved, it was not added.
    assert.equal(p.modal.querySelectorAll('.subtitle[data-default="true"]').length, 1);

    assert.equal(p.puts().length, 1);
    const put = p.puts()[0];
    assert.equal(put.url, '/stream-video/subtitle');
    assert.equal(put.params.method, 'PUT');
    assert.equal(put.params.headers['X-CSRF-TOKEN'], 'csrf-token');
    assert.deepEqual(put.body, { id: 'os-os-en', resourceID: 'res', itemID: 'item' });

    // The track was side-loaded on selection rather than shipped in the
    // page, and its <track label> is the plain name — not the decorated
    // chip text (the native iOS menu shows this string).
    const track = p.video.querySelector('track#os-os-en');
    assert.ok(track, 'a side-loaded track is created on first selection');
    assert.equal(track.getAttribute('label'), 'Movie.en.srt');
    assert.equal(p.video.textTracks.find((t) => t.id === 'os-os-en').mode, 'showing');

    assert.deepEqual(p.hooks.selected, ['os-os-en'], 'the component hears a manual choice');
    assert.ok(p.events.some((e) => e.name === 'subtitle-select'));
});

test('a click landing on a chip’s inner span still selects the chip', async () => {
    const p = mount();
    p.wire();
    const dub = p.audioChip('mp-1');
    const inner = dub.querySelector('span');
    assert.ok(inner, 'the audio chip must have an inner span to click');

    click(inner);
    await flush();

    assert.equal(dub.getAttribute('data-default'), 'true', 'closest() found the chip, not the span');
    assert.ok(active(dub));
    assert.equal(p.audioChip('mp-0').getAttribute('data-default'), null);
    assert.deepEqual(p.puts().map((c) => c.url), ['/stream-video/audio']);
    assert.deepEqual(p.puts()[0].body, { id: 'mp-1', resourceID: 'res', itemID: 'item' });
    assert.deepEqual(p.hooks.audio, ['mp-1']);
});

// ---- the switch ------------------------------------------------------

test('the switch turns subtitles on with the suggested track, and off again', async () => {
    const p = mount();
    p.wire();
    const toggle = p.modal.querySelector('#subtitles-toggle');
    assert.equal(p.modal.getAttribute('data-subtitles-off'), 'true', 'the fixture opens with subtitles off');

    // On: the server named what comes back (data-suggested) and nothing
    // else has been chosen this session.
    toggle.checked = true;
    toggle.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
    await flush();

    assert.equal(p.modal.getAttribute('data-subtitles-off'), 'false');
    assert.equal(p.chip('mp-0').getAttribute('data-default'), 'true');
    assert.equal(p.chip('none').getAttribute('data-default'), null);
    assert.equal(p.puts().length, 1, 'one activation, one PUT');
    assert.deepEqual(p.puts()[0].body.id, 'mp-0');

    // Off: the "None" carrier becomes the default, the outgoing track is
    // remembered, and the block is muted rather than emptied.
    toggle.checked = false;
    toggle.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
    await flush();

    assert.equal(p.modal.getAttribute('data-subtitles-off'), 'true');
    assert.equal(p.modal.getAttribute('data-last-subtitle'), 'mp-0');
    assert.equal(p.chip('none').getAttribute('data-default'), 'true');
    assert.ok(active(p.chip('mp-0')), 'the track that comes back keeps the mark');
    assert.equal(p.chip('mp-0').getAttribute('aria-checked'), 'true');
    assert.ok(p.modal.querySelector('#subtitle-tracks').classList.contains('picker-off'));
    assert.equal(p.chip('os-os-en').getAttribute('aria-disabled'), 'true');
    assert.equal(p.puts().length, 2);
    assert.equal(p.puts()[1].body.id, 'none');

    // The "None" carrier never takes the look, however often it is
    // activated: an invisible chip drawn as the chosen one, in a group
    // where nothing appears checked, is the worst of both.
    assert.equal(active(p.chip('none')), false);

    // And back on: the same track, through the session memory this time.
    toggle.checked = true;
    toggle.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
    await flush();
    assert.equal(p.chip('mp-0').getAttribute('data-default'), 'true');
    assert.equal(p.puts().length, 3);
});

test('clicking a chip while subtitles are off is one act: on, with that track', async () => {
    const p = mount();
    p.wire();
    assert.equal(p.modal.getAttribute('data-subtitles-off'), 'true');

    click(p.chip('os-os-ru'));
    await flush();

    assert.equal(p.modal.getAttribute('data-subtitles-off'), 'false', 'the switch follows the activation');
    assert.equal(p.modal.querySelector('#subtitles-toggle').checked, true);
    assert.ok(active(p.chip('os-os-ru')));
    assert.equal(p.puts().length, 1, 'one act, one PUT');
    assert.equal(p.puts()[0].body.id, 'os-os-ru');
});

// ---- the language row ------------------------------------------------

test('a language chip filters and changes nothing else', async () => {
    const p = mount();
    p.wire();
    const before = p.modal.querySelector('.subtitle[data-default="true"]');

    click(p.lang('ru'));
    await flush();

    assert.equal(p.lang('ru').getAttribute('aria-pressed'), 'true');
    assert.equal(p.lang('pt').getAttribute('aria-pressed'), 'false');
    assert.equal(p.chip('os-os-ru').hidden, false);
    assert.equal(p.chip('os-os-en').hidden, true);
    assert.equal(p.chip('none').hidden, true, 'the carrier is hidden in every language');
    assert.equal(p.modal.querySelector('.subtitle[data-default="true"]'), before, 'playback is untouched');
    assert.equal(p.puts().length, 0, 'a filter is not a choice');
});

test('"+N" is a toggle, not a one-way reveal', async () => {
    const p = mount();
    p.wire();
    const more = p.modal.querySelector('#subtitle-lang-more');
    const label = more.querySelector('.more-count');
    const collapsed = label.textContent;
    assert.match(collapsed, /^\+[1-9]/, 'the fixture must overflow the row');
    const hiddenBefore = Array.from(p.modal.querySelectorAll('.lang[data-lang]')).filter((el) => el.hidden).length;
    assert.ok(hiddenBefore > 0);

    click(more);
    await flush();
    assert.equal(more.getAttribute('aria-expanded'), 'true');
    assert.equal(label.textContent, '×');
    assert.equal(Array.from(p.modal.querySelectorAll('.lang[data-lang]')).filter((el) => el.hidden).length, 0);

    click(more);
    await flush();
    assert.equal(more.getAttribute('aria-expanded'), 'false');
    assert.equal(label.textContent, collapsed);
    assert.equal(Array.from(p.modal.querySelectorAll('.lang[data-lang]')).filter((el) => el.hidden).length, hiddenBefore);
});

// ---- the uploads swap ------------------------------------------------

// asyncSwap is what loadAsyncView leaves behind: #my-subtitles' innerHTML
// replaced by the partial's response, then an 'async' CustomEvent naming
// that element.
//
// The response is a Go-rendered fixture, not markup written here. Writing it
// by hand was the one place the drift this whole machinery exists to stop
// was still possible — and the worst place for it, since the chip
// attributes it carries (data-lang, data-provider, data-rank,
// data-autoselect) are exactly what adoptUploadChips, the language row and
// the filter read. A hand-written copy would have stayed green while the
// partial stopped emitting one of them.
//
//   'upload' — two uploads, the second freshly added (Selected, so it
//              carries data-autoselect). us-1 is also in the dialog
//              fixture, so replaying this exercises replace-in-place and
//              not only the add path.
//   'empty'  — the list after the last file was deleted. An empty
//              #my-upload-chips is an answer, not an absence.
const ASYNC = {
    upload: readFileSync(path.join(HERE, '__fixtures__/user-subtitles-async.html'), 'utf8'),
    empty: readFileSync(path.join(HERE, '__fixtures__/user-subtitles-async-empty.html'), 'utf8'),
};

function asyncSwap(modal, which) {
    const wrap = modal.querySelector('#my-subtitles');
    wrap.innerHTML = ASYNC[which];
    window.dispatchEvent(new dom.window.CustomEvent('async', { detail: { target: wrap } }));
}

test('an upload lands in the radiogroup, selected, exactly once', async () => {
    const p = mount();
    p.wire();
    assert.ok(p.chip('us-1'), 'the fixture carries one upload already');

    asyncSwap(p.modal, 'upload');
    await flush();

    const row = p.modal.querySelector('#subtitle-tracks');
    const fresh = p.chip('us-2');
    assert.ok(fresh, 'the fresh chip exists');
    assert.equal(fresh.parentElement, row, 'and it is in the radiogroup, not in #my-subtitles');
    assert.equal(p.chip('us-1').parentElement, row);
    // No duplicate of the id the response re-rendered.
    assert.equal(p.modal.querySelectorAll('.subtitle[data-id="us-1"]').length, 1);
    // Nothing is left behind in the wrapper, and the marker is consumed.
    assert.equal(p.modal.querySelectorAll('#my-subtitles .subtitle').length, 0);
    assert.equal(p.modal.querySelector('#my-upload-chips'), null);

    // Autoselect: the viewer uploaded a file to watch with.
    assert.ok(active(fresh));
    assert.equal(fresh.getAttribute('data-default'), 'true');
    assert.equal(p.modal.querySelectorAll('.subtitle[data-default="true"]').length, 1);
    assert.equal(p.modal.getAttribute('data-subtitles-off'), 'false');
    assert.deepEqual(p.puts().map((c) => c.body.id), ['us-2']);

    // The row caught up with the new chip: English now counts three.
    assert.equal(p.lang('en').querySelector('.lang-count').textContent, '3');

    // And the MY block is where the server puts it — before the AI item,
    // since an upload is rank 0. Appending instead would move the uploads
    // past the AI chip on every upload and delete, and the next page load
    // would move them back: the row reordering for reasons unrelated to
    // anything the viewer did.
    const order = Array.from(p.modal.querySelectorAll('#subtitle-tracks .subtitle[data-id]'))
        .map((el) => el.getAttribute('data-id'));
    assert.deepEqual(order.slice(-3), ['us-1', 'us-2', 'tr-pt']);
});

test('deleting the upload that is playing lands on Off, without a PUT', async () => {
    const p = mount({ tracks: [] });
    p.wire();

    click(p.chip('us-1'));
    await flush();
    assert.equal(p.puts().length, 1);
    assert.ok(p.video.querySelector('track#us-1'), 'selecting it side-loaded the track');

    // The delete response: the list is empty now.
    asyncSwap(p.modal, 'empty');
    await flush();

    assert.equal(p.chip('us-1'), null, 'the chip left the row with the file');
    assert.equal(p.video.querySelector('track#us-1'), null, 'and so did the orphaned <track>');
    assert.equal(p.modal.getAttribute('data-subtitles-off'), 'true');
    assert.equal(p.chip('none').getAttribute('data-default'), 'true');
    // The viewer chose a deletion, not a track: nothing new is persisted.
    assert.equal(p.puts().length, 1);
});

test('deleting an upload that is not playing leaves playback alone', async () => {
    const p = mount();
    p.wire();
    click(p.chip('os-os-en'));
    await flush();

    asyncSwap(p.modal, 'empty');
    await flush();

    assert.equal(p.chip('us-1'), null);
    assert.equal(p.chip('os-os-en').getAttribute('data-default'), 'true');
    assert.equal(p.modal.getAttribute('data-subtitles-off'), 'false');
    assert.equal(p.puts().length, 1);
});

test('the uploads panel survives the swap it is replaced by', async () => {
    const p = mount();
    p.wire();

    click(p.modal.querySelector('#my-uploads-toggle'));
    await flush();
    assert.equal(p.modal.querySelector('#my-uploads-panel').hidden, false);
    assert.equal(p.modal.querySelector('#my-subtitles').getAttribute('data-upload-open'), 'true');

    // A delete replaces the toggle and the panel; the wrapper, which is
    // what remembers, is not replaced.
    asyncSwap(p.modal, 'upload');
    await flush();
    assert.equal(p.modal.querySelector('#my-uploads-panel').hidden, false,
        'removing two files in a row must not mean re-opening the panel between them');

    // The panel's own "×" closes it exactly as the chip does.
    click(p.modal.querySelector('#my-uploads-toggle'));
    await flush();
    assert.equal(p.modal.querySelector('#my-uploads-panel').hidden, true);
    assert.equal(p.modal.querySelector('#my-subtitles').getAttribute('data-upload-open'), 'false');
});

// ---- syncUploadMarks -------------------------------------------------

test('syncUploadMarks marks the upload that is playing, and only then', async () => {
    const p = mount({ tracks: [['us-1', true]] });
    syncUploadMarks(p.container, p.modal);

    assert.ok(active(p.chip('us-1')), 'the <track default> says the upload is what plays');
    assert.equal(p.chip('us-1').getAttribute('data-default'), 'true');
    assert.equal(p.modal.querySelectorAll('.subtitle[data-default="true"]').length, 1);
});

test('syncUploadMarks refuses a stale default when an embedded track is playing', async () => {
    // An embedded track is driven by hls.js and has no <track> element of
    // its own, so "no showing textTrack" is also what it playing looks
    // like. Reading the stale `default` attribute as the answer there
    // handed the marker to the upload as well and left two chips checked.
    //
    // Reached the way a viewer reaches it: the page shipped the upload as
    // the preloaded default, then the viewer picked the embedded Japanese
    // track, which disables every side-loaded <track> and leaves the
    // `default` attribute behind.
    const p = mount({ tracks: [['us-1', true]] });
    p.wire();
    click(p.chip('mp-0'));
    await flush();
    assert.equal(p.video.querySelector('track#us-1').hasAttribute('default'), true, 'the stale attribute is still there');
    assert.equal(p.video.textTracks.find((t) => t.id === 'us-1').mode, 'disabled');

    syncUploadMarks(p.container, p.modal);

    assert.equal(p.chip('us-1').getAttribute('data-default'), null, 'the upload must not claim the marker');
    assert.equal(active(p.chip('us-1')), false);
    assert.equal(p.modal.querySelectorAll('.subtitle[data-default="true"]').length, 1);
    assert.equal(p.chip('mp-0').getAttribute('data-default'), 'true');
});

// ---- persisting the choice ------------------------------------------

test('a 503 on the PUT is retried once, and the retry lands', async () => {
    const p = mount();
    p.wire();
    p.setResponse((url, params, n) => (n === 1
        ? { ok: false, status: 503 }
        : { ok: true, status: 200 }));

    const startedAt = Date.now();
    await markTrack(p.container, p.chip('os-os-en'), 'subtitle');

    const puts = p.puts();
    assert.equal(puts.length, 2, 'the choice is not lost to an edge blip');
    assert.deepEqual(puts[1].body, puts[0].body, 'the retry sends the same choice');
    // Deferred, not immediate: an instant resend arrives at the same edge
    // that just refused, and is the stampede this deliberately is not.
    assert.ok(Date.now() - startedAt >= PUT_RETRY_DELAY_MS - 50,
        `the retry waited ${Date.now() - startedAt} ms, expected about ${PUT_RETRY_DELAY_MS}`);
});

test('a network error is retried once too, and a second failure gives up', async () => {
    const p = mount();
    p.wire();
    let n = 0;
    const rejecting = () => { n++; return Promise.reject(new Error('ECONNRESET')); };
    const restore = globalThis.fetch;
    globalThis.fetch = window.fetch = rejecting;
    try {
        await markTrack(p.container, p.chip('os-os-en'), 'subtitle');
    } finally {
        // Put it back rather than leaving a permanently rejecting fetch
        // installed. Every later test calls mount() first today, so this
        // is safe either way — which is exactly why it would be missed the
        // day one does not.
        globalThis.fetch = window.fetch = restore;
    }

    assert.equal(n, 2, 'one retry, not a stampede');
});

test('a 4xx is an answer, not a blip: no retry', async () => {
    const p = mount();
    p.wire();
    p.setResponse(() => ({ ok: false, status: 403 }));

    await markTrack(p.container, p.chip('os-os-en'), 'subtitle');

    assert.equal(p.puts().length, 1, 'repeating a refused request gets it refused again');
});

test('a retry stands down when a newer choice has already been written', async () => {
    // The failure the sequence guard exists for. Click A, it 503s and
    // queues a retry; click B inside the retry window and it succeeds. If
    // A's retry still fired, the session would end up holding A and the
    // next page load would restore the wrong track — a corruption the old
    // fire-and-forget PUT could not cause, because a lost write only lost
    // itself.
    const p = mount();
    p.wire();
    p.setResponse((url, params, n) => (n === 1 ? { ok: false, status: 503 } : { ok: true, status: 200 }));

    const first = markTrack(p.container, p.chip('os-os-en'), 'subtitle');
    // Inside the retry window, and it wins.
    const second = markTrack(p.container, p.chip('os-os-ru'), 'subtitle');
    // Awaiting both is already past the point A's retry would have fired:
    // the returned promise resolves after the delay, having decided.
    await Promise.all([first, second]);

    const puts = p.puts();
    assert.equal(puts.length, 2, 'two clicks, two writes -- the stale retry was skipped');
    assert.deepEqual(puts.map((c) => c.body.id), ['os-os-en', 'os-os-ru']);
    assert.equal(puts[puts.length - 1].body.id, 'os-os-ru', 'the session ends up holding the newer choice');
});

test('a retry does not outlive the player that queued it', async () => {
    const p = mount();
    p.wire();
    p.setResponse(() => ({ ok: false, status: 503 }));

    const done = markTrack(p.container, p.chip('os-os-en'), 'subtitle');
    destroyPlayer();
    await done;

    assert.equal(p.puts().length, 1, 'a write queued by a page the viewer left must not land');
});

test('audio and subtitle are sequenced apart', async () => {
    // Two independent choices. A subtitle click must not cancel the retry
    // of an audio write made a moment earlier, or the guard would turn
    // "pick a track, then pick a language" into a lost audio choice.
    const p = mount();
    p.wire();
    p.setResponse((url, params, n) => (n === 1 ? { ok: false, status: 503 } : { ok: true, status: 200 }));

    const audio = markTrack(p.container, p.audioChip('mp-1'), 'audio');
    const subtitle = markTrack(p.container, p.chip('os-os-en'), 'subtitle');
    await Promise.all([audio, subtitle]);

    const puts = p.puts();
    assert.equal(puts.length, 3, 'the audio retry still fires');
    assert.deepEqual(puts.map((c) => c.url), [
        '/stream-video/audio', '/stream-video/subtitle', '/stream-video/audio',
    ]);
});

// ---- the translation offer ------------------------------------------

test('an offered translation is a verb until it is the track playing', async () => {
    const p = mount();
    p.wire();
    const ai = p.chip('tr-pt');
    assert.ok(ai, 'the fixture must offer a translation');
    assert.equal(ai.getAttribute('data-offered'), 'true');
    assert.ok(ai.classList.contains('chip-offered'));
    assert.equal(ai.querySelector('.ai-action').hidden, false);
    assert.equal(ai.querySelector('.ai-label').hidden, true);

    click(ai);
    await flush();

    // An offer taken is an offer spent.
    assert.equal(ai.getAttribute('data-offered'), null);
    assert.equal(ai.classList.contains('chip-offered'), false);
    assert.equal(ai.querySelector('.ai-action').hidden, true);
    assert.equal(ai.querySelector('.ai-label').hidden, false);
    assert.ok(active(ai));
    // ...and the sentence that explained the offer stops being shown.
    assert.equal(p.modal.querySelector('#subtitle-hint').hidden, true);
    assert.deepEqual(p.hooks.selected, ['tr-pt'], 'the component is told to start the run');
});

test('a locked translation sells instead of switching', async () => {
    const p = mount();
    p.wire();
    const ai = p.chip('tr-pt');
    // The free-viewer render: locked, no Src.
    ai.setAttribute('data-locked', 'true');
    ai.removeAttribute('data-src');
    const before = p.modal.querySelector('.subtitle[data-default="true"]');

    click(ai);
    await flush();

    assert.equal(p.modal.querySelector('#translate-cta').hidden, false, 'the upgrade card is revealed');
    assert.equal(active(ai), false, 'a locked chip can never take the mark');
    assert.equal(p.modal.querySelector('.subtitle[data-default="true"]'), before);
    assert.equal(p.puts().length, 0);
    assert.deepEqual(p.hooks.selected, [], 'nothing was selected, so no run starts');
    assert.ok(p.events.some((e) => e.name === 'subtitle-translate-lock-click'));
});

// ---- the translation run ---------------------------------------------
//
// Everything above drives wireTrackHandlers with a stand-in `hooks` object,
// because the wiring only calls through it. Starting a translation is the
// other side of that seam: it lives in the mounted component, which fills
// the hooks in. These three tests therefore mount the real thing through
// initPlayer — the same entry point the page uses — so the chain under test
// is the whole one, from a click on a chip to a poll and an event.

// mountPlayer builds the page, then runs initPlayer on it. The DOM is
// rearranged on the way (the video is wrapped, the Preact controls are
// rendered), so the handles are re-read afterwards.
async function mountPlayer(prepare, opts) {
    const p = mount(opts);
    if (prepare) prepare(p);
    await initPlayer(p.container);
    // preact defers effects to the next animation frame (jsdom's fires on a
    // ~16 ms timer), and the mount-time restore of a saved translation runs
    // inside one. Waiting on the frame rather than on a microtask is what
    // makes this deterministic.
    await settle();
    return p;
}

// progressResponse is what the translate service answers a HEAD with.
const progressResponse = (header) => ({
    ok: true,
    status: 200,
    headers: { get: (n) => (n === 'X-Subtitle-Progress' ? header : null) },
    json: async () => ({}),
});

// liveProgressResponse is the same, but for a still-growing embedded-track
// source: X-Subtitle-Live marks `header`'s total as a snapshot, not a
// ceiling.
const liveProgressResponse = (header) => ({
    ok: true,
    status: 200,
    headers: { get: (n) => (n === 'X-Subtitle-Progress' ? header : (n === 'X-Subtitle-Live' ? '1' : null)) },
    json: async () => ({}),
});

test('clicking the AI chip starts a run: one start event, a poll, a percentage', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? progressResponse('12/400')
        : { ok: true, status: 200, json: async () => ({}) }));

    const ai = p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]');
    const src = ai.getAttribute('data-src');
    // Playing: a run polls only while somebody is watching, and jsdom's
    // <video> is paused until a test says otherwise.
    p.video.paused = false;
    click(ai);
    await settle();

    const starts = p.events.filter((e) => e.name === 'subtitle-translate-start');
    assert.equal(starts.length, 1, 'exactly one start for one click');
    assert.equal(starts[0].data.lang, 'pt');
    assert.equal(starts[0].data.source, ai.getAttribute('data-source-badge'),
        'the event names the human track being translated');

    // The poll is real: a HEAD against the item's own src.
    const heads = p.calls.filter((c) => c.params && c.params.method === 'HEAD');
    assert.ok(heads.length >= 1, 'the run polls');
    // Against the item's own src, plus the playhead every poll carries so
    // a file job can order its batches by it.
    assert.equal(heads[0].url, `${src}?pos=0`);

    // And the chip says so, without its markup being rebuilt — the
    // progress span was in the template all along.
    assert.equal(ai.querySelector('.tr-progress').hidden, false);
    assert.equal(ai.querySelector('.tr-progress').textContent, '· 3%');
    assert.equal(ai.querySelector('.tr-spinner').hidden, false);
    assert.ok(ai.querySelector('.chip-origin'), 'the chip kept its badge');
});

test('a live source (X-Subtitle-Live) shows a count instead of a percent', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? liveProgressResponse('3/3')
        : { ok: true, status: 200, json: async () => ({}) }));

    const ai = p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]');
    p.video.paused = false;
    click(ai);
    await settle();

    // done == total does not mean final while the source is still live:
    // the poll keeps running rather than hiding the chip.
    const span = ai.querySelector('.tr-progress');
    assert.equal(span.hidden, false);
    assert.equal(span.textContent, '· 3', 'a count, not a percentage');
    assert.equal(span.title, 'player.subtitleTranslatingLive');
});

// ---- the stream-start measurement ------------------------------------

// playPast drives the rAF loop in usePlayerState past the engagement gate
// (5 s of playback), which is what emits stream-start and subtitle-resolved.
async function playPast(p, seconds = 6) {
    p.video.paused = false;
    p.video.currentTime = seconds;
    await settle();
}

test('subtitle-resolved says when the OpenSubtitles lookup never finished', async (t) => {
    t.after(() => destroyPlayer());
    // The server sets this when video-info answered "not ready" twice: the
    // page renders without those tracks and is cached for ten minutes like
    // any other, so without the field a level of 'none' would count a file
    // nobody got to look at as a file with nothing to find.
    const p = await mountPlayer((page) => {
        page.modal.setAttribute('data-subtitles-not-ready', 'true');
    });
    await playPast(p);

    const ev = p.events.find((e) => e.name === 'subtitle-resolved');
    assert.ok(ev, 'the engagement gate must emit subtitle-resolved');
    assert.equal(ev.data.notReady, true);
});

test('subtitle-resolved reports a finished lookup as such', async (t) => {
    t.after(() => destroyPlayer());
    // The negative control: the attribute is absent on an ordinary render,
    // and the field must then be false rather than missing — a field that
    // is only ever present in one of the two cases cannot be filtered on.
    const p = await mountPlayer();
    await playPast(p);

    const ev = p.events.find((e) => e.name === 'subtitle-resolved');
    assert.ok(ev, 'the engagement gate must emit subtitle-resolved');
    assert.equal(ev.data.notReady, false);
});

// ---- the codec-support measurement -------------------------------------
//
// codec-support.js has the rules; these check the player hands it the right
// moment and the right stream. The page flag and the weekly stamp are reset
// first: an earlier test's `playing` may have used up this page's report.

const codecEvents = (p) => p.events.filter((e) => e.name === 'codec-support');
const freshBrowser = () => {
    delete window.__wtCodecSupport;
    window.localStorage.removeItem('wt-codec-support');
};

test('codec-support goes out on the first frame, not on mount, with the source codec', async (t) => {
    t.after(() => destroyPlayer());
    freshBrowser();
    const p = await mountPlayer((page) => {
        // As stream_video.html writes it: every video stream of the probe,
        // a cover picture among them.
        page.video.setAttribute('data-video-codecs', 'mjpeg hevc ');
    });
    p.video.dispatchEvent(new dom.window.Event('loadedmetadata'));
    p.video.dispatchEvent(new dom.window.Event('canplay'));
    p.video.dispatchEvent(new dom.window.Event('play'));
    await settle();
    assert.equal(codecEvents(p).length, 0, 'mounted, loaded and asked to play is not watching');

    p.video.dispatchEvent(new dom.window.Event('playing'));
    await settle();
    const got = codecEvents(p);
    assert.equal(got.length, 1);
    assert.equal(got[0].data.src, 'hevc');
    assert.equal(got[0].data.tc, false, 'no data-session-id: not a transcoder session');
    assert.equal(got[0].data.pl, 'direct', 'no HLS source in this harness');
    assert.equal(got[0].data.emb, false);
    assert.equal(got[0].data.mse, 'none', 'jsdom has no MSE; the probe ran against the page');

    p.video.dispatchEvent(new dom.window.Event('playing'));
    await settle();
    assert.equal(codecEvents(p).length, 1, 'a replayed `playing` is not a second viewer');
});

test('codec-support: a page without a probe reports the source as unknown', async (t) => {
    t.after(() => destroyPlayer());
    freshBrowser();
    const p = await mountPlayer();
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await settle();
    const got = codecEvents(p);
    assert.equal(got.length, 1);
    assert.equal(got[0].data.src, 'unknown');
});

test('codec-support: the audio player does not report', async (t) => {
    t.after(() => destroyPlayer());
    freshBrowser();
    const p = await mountPlayer(null, { tag: 'audio' });
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await settle();
    assert.equal(codecEvents(p).length, 0);
});

test('a stopped run keeps its count, loses its spinner, and is reported', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    // The service stopped this live run incomplete (source_gone /
    // too_large). The counts in that answer are the last ones there will
    // ever be, and those cues are on screen.
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? {
            ok: true,
            status: 200,
            headers: {
                get: (n) => {
                    if (n === 'X-Subtitle-Progress') return '11/40';
                    if (n === 'X-Subtitle-Live') return '1';
                    if (n === 'X-Subtitle-Status') return 'stopped';
                    return null;
                },
            },
            json: async () => ({}),
        }
        : { ok: true, status: 200, json: async () => ({}) }));

    const ai = p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]');
    p.video.paused = false;
    click(ai);
    await settle();

    const span = ai.querySelector('.tr-progress');
    // The count stays: it is what the viewer actually got. The spinner is
    // the part that claims work is still happening, so it goes.
    assert.equal(span.hidden, false, 'the last count survives a stop');
    assert.equal(span.textContent, '· 11');
    assert.equal(span.title, 'player.subtitleTranslationStopped');
    assert.equal(ai.querySelector('.tr-spinner').hidden, true);

    const errs = p.events.filter((e) => e.name === 'subtitle-translate-error');
    assert.equal(errs.length, 1, 'one report for one stop');
    assert.equal(errs[0].data.code, 'stopped');
    assert.equal(errs[0].data.lang, 'pt');
    assert.deepEqual(p.events.filter((e) => e.name === 'subtitle-translate-done'), [],
        'stopped is not done');
});

test('re-clicking a stopped translation does nothing at all', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? {
            ok: true,
            status: 200,
            headers: {
                get: (n) => {
                    if (n === 'X-Subtitle-Progress') return '11/40';
                    if (n === 'X-Subtitle-Live') return '1';
                    if (n === 'X-Subtitle-Status') return 'stopped';
                    return null;
                },
            },
            json: async () => ({}),
        }
        : { ok: true, status: 200, json: async () => ({}) }));

    const ai = p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]');
    p.video.paused = false;
    click(ai);
    await settle();
    const headsAfterStop = p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;
    assert.ok(headsAfterStop >= 1, 'the first click must have polled');

    // Away and back: the run is over, and the chip's own copy says the
    // way to retry is a reload. Re-selecting must not poll again, and
    // must not emit a second error for the same dead run.
    click(p.chip('os-os-en'));
    await settle();
    click(ai);
    await settle();

    assert.equal(p.calls.filter((c) => c.params && c.params.method === 'HEAD').length, headsAfterStop,
        'a stopped translation must not be polled again');
    assert.equal(p.events.filter((e) => e.name === 'subtitle-translate-error').length, 1,
        'nor reported again');
    assert.equal(p.events.filter((e) => e.name === 'subtitle-translate-start').length, 1,
        'and it is not a new run either');
});

test('nothing starts a translation on its own', async (t) => {
    t.after(() => destroyPlayer());
    // The fixture is the 2026-09-16 state: the AI item is Offered and the
    // server did not make it Default. The engagement-gate auto-start is
    // gone, so a mount must spend no tokens.
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? progressResponse('12/400')
        : { ok: true, status: 200, json: async () => ({}) }));
    await settle();

    assert.deepEqual(p.events.filter((e) => e.name === 'subtitle-translate-start'), []);
    assert.deepEqual(p.calls.filter((c) => c.params && c.params.method === 'HEAD'), []);
    assert.equal(p.puts().length, 0, 'and nothing was persisted either');
});

// ---- the audio switch -------------------------------------------------

test('an audio switch with no preferred language leaves the subtitles alone', async (t) => {
    // data-preferred-lang is empty in two configurations that are live
    // today: every embed, and any deployment with
    // SUBTITLE_TRANSLATE_ENABLED off — subtitleOptsFor
    // (jobs/scripts/translate_opts.go) returns the zero SubtitleOpts for
    // both. The rule used to answer 'none' there, and since the
    // activation is persist:false, a first-time viewer lost their
    // subtitles to the audio menu with nothing recording why.
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => {
        it.modal.setAttribute('data-preferred-lang', '');
        // Subtitles on, on a ladder pick rather than a saved choice: the
        // one state this rule is allowed to re-decide at all.
        it.modal.setAttribute('data-subtitles-off', 'false');
        it.chip('none').removeAttribute('data-default');
        it.chip('os-os-en').setAttribute('data-default', 'true');
    });
    const modal = p.container.querySelector('#subtitles');
    const sub = (id) => modal.querySelector(`.subtitle[data-id="${id}"]`);

    click(modal.querySelector('.audio[data-id="mp-1"]'));
    await settle();

    assert.equal(sub('os-os-en').getAttribute('data-default'), 'true',
        'the subtitle that was playing is still playing');
    assert.equal(sub('none').getAttribute('data-default'), null);
    assert.equal(modal.getAttribute('data-subtitles-off'), 'false');
    // The audio choice itself still lands — the rule is inert, not the click.
    assert.equal(modal.querySelector('.audio[data-id="mp-1"]').getAttribute('data-default'), 'true');
    assert.deepEqual(p.puts().map((c) => c.url), ['/stream-video/audio'],
        'the audio write and nothing else: no subtitle was re-decided');
});

// The GET that creates the <track> is the request that starts the job, and
// the service orders its first batch by the position on record. Measured
// 2026-09-18: with no position on that GET it used what an earlier viewing
// of the same file had left, and a film opened at 0:00 got its first ~50
// cues from the sixth minute.
test('the request that starts a translation says where the viewer is', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? progressResponse('0/0')
        : { ok: true, status: 200, json: async () => ({}) }));
    p.video.paused = false;
    p.video.currentTime = 754.8;
    click(p.chip('tr-pt'));
    await settle();
    const src = p.video.querySelector('track#tr-pt').getAttribute('src') || '';
    assert.match(src, /[?&]pos=754(&|$)/, `movie time, floored like a poll's: ${src}`);
});

test('in a transcoder session that position is movie time, not run time', async (t) => {
    // Its own mount: the session helper pre-renders the <track>, and the
    // server never does that for a translation (markPreload skips it).
    t.after(() => destroyPlayer());
    const p = mount();
    p.video.dataset.sessionId = 's1';
    p.video.dataset.sessionSeekUrl = '/session/seek';
    p.setResponse((url, params) => {
        if (params && params.method === 'HEAD') return catchUpResponse('0/0', null);
        return { ok: true, status: 200, json: async () => ({ offset: 600 }) };
    });
    await initPlayer(p.container);
    await settle();
    p.video.paused = false;
    p.video.currentTime = 12.4;
    click(p.chip('tr-pt'));
    await settle();
    const src = p.video.querySelector('track#tr-pt').getAttribute('src') || '';
    assert.match(src, /[?&]pos=612(&|$)/, `600 s run offset + 12 s into it: ${src}`);
});

test('a track that is not a translation is requested as the server wrote it', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer(null, { tracks: [] });
    p.video.currentTime = 754.8;
    const os = Array.from(p.container.querySelectorAll('#subtitles .subtitle'))
        .find((el) => el.getAttribute('data-provider') !== 'Translated' && el.getAttribute('data-provider') !== 'MediaProbe'
            && el.getAttribute('data-id') !== 'none' && el.getAttribute('data-src'));
    assert.ok(os, 'fixture: a side-loaded, non-AI chip');
    click(os);
    await settle();
    const el = Array.from(p.video.querySelectorAll('track')).find((x) => x.id === os.getAttribute('data-id'));
    assert.equal(el.getAttribute('src'), os.getAttribute('data-src'));
});

test('a translation saved in an earlier session comes back once, unpersisted', async (t) => {
    t.after(() => destroyPlayer());
    // The one automatic start left. A translation is not a <track> in the
    // page (markPreload skips it), so unlike every other saved choice it
    // cannot resume by itself. It is the viewer's own choice coming back,
    // which is what justifies replaying it — not that it is cached: an
    // embedded source makes this a live job, and what bounds that is the
    // poll sleeping with the video.
    const p = await mountPlayer((it) => {
        it.setResponse((url, params) => (params && params.method === 'HEAD'
            ? progressResponse('400/400')
            : { ok: true, status: 200, json: async () => ({}) }));
        // Playing by the time the restore runs — autoplay did what the
        // template asks for. When it does not, the restored run starts
        // asleep instead; that is its own test.
        it.video.paused = false;
        const ai = it.chip('tr-pt');
        ai.setAttribute('data-saved', 'true');
        ai.setAttribute('data-default', 'true');
        ai.removeAttribute('data-offered');
        it.chip('none').removeAttribute('data-default');
        it.modal.setAttribute('data-subtitles-off', 'false');
    });

    const starts = p.events.filter((e) => e.name === 'subtitle-translate-start');
    assert.equal(starts.length, 1, 'restored exactly once');
    assert.equal(starts[0].data.lang, 'pt');
    // persist: false — nothing was chosen here that was not already chosen,
    // so the saved marker must not be rewritten as a fresh choice.
    assert.equal(p.puts().length, 0);

    // A second render pass must not restore it again.
    await settle();
    assert.equal(p.events.filter((e) => e.name === 'subtitle-translate-start').length, 1);
    // The cached file finishes at once, and the run reports itself done.
    assert.equal(p.events.filter((e) => e.name === 'subtitle-translate-done').length, 1);
});

// The poll interval inside the player is not injectable (pollProgress's
// 3 s default), so seeing that a tick did NOT happen means outwaiting one
// whole interval. Everything else here is immediate.
const POLL_INTERVAL_WINDOW_MS = 3300;
const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
// jsdom has no way to change visibility, and `hidden` is a prototype
// getter; an own configurable property shadows it, and `delete` in the
// test's t.after puts jsdom's back.
const setHidden = (v) => {
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => v });
    document.dispatchEvent(new dom.window.Event('visibilitychange'));
};

test('the translation poll sleeps with the video and wakes with it', async (t) => {
    // The HEAD every 3 s is what tells the translate service somebody is
    // watching, and for a live source it holds the transcoder session (and
    // its FFmpeg run) open behind it. A paused or hidden viewer is not
    // watching, so the poll stops — without giving up: no error, no
    // telemetry, the chip keeps its count, and the same run continues.
    t.after(() => { destroyPlayer(); delete document.hidden; });
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? liveProgressResponse('3/3')
        : { ok: true, status: 200, json: async () => ({}) }));
    const heads = () => p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;

    const ai = p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]');
    p.video.paused = false;
    click(ai);
    await settle();
    assert.equal(heads(), 1, 'the run polls on start');

    p.video.paused = true;
    p.video.dispatchEvent(new dom.window.Event('pause'));
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(heads(), 1, 'a paused viewer pays for no polls');
    // Asleep, not dead — which is the whole difference from stopping it.
    assert.equal(ai.querySelector('.tr-progress').hidden, false);
    assert.equal(ai.querySelector('.tr-progress').textContent, '· 3', 'the chip keeps its last count');
    assert.equal(ai.querySelector('.tr-spinner').hidden, false);
    assert.deepEqual(p.events.filter((e) => e.name === 'subtitle-translate-error'), [],
        'sleeping is not a failure and must not be counted as one');

    p.video.paused = false;
    p.video.dispatchEvent(new dom.window.Event('play'));
    await settle();
    assert.equal(heads(), 2, 'play wakes the same run at once');

    setHidden(true);
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(heads(), 2, 'a hidden tab pays for no polls either');
    setHidden(false);
    await settle();
    assert.equal(heads(), 3, 'and coming back wakes it');

    // One run throughout: waking is not a new translation, so the start
    // event (and the tokens behind it) happens once.
    assert.equal(p.events.filter((e) => e.name === 'subtitle-translate-start').length, 1);
});

test('coming back to a tab whose video is still paused does not wake the poll', async (t) => {
    // The negative control for the resume side: `visible` alone is not
    // "watching". Without the paused check, tabbing back to a paused film
    // would put the transcoder session back on the clock.
    t.after(() => { destroyPlayer(); delete document.hidden; });
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? liveProgressResponse('3/3')
        : { ok: true, status: 200, json: async () => ({}) }));
    const heads = () => p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;

    p.video.paused = false;
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();
    assert.equal(heads(), 1);

    p.video.paused = true;
    p.video.dispatchEvent(new dom.window.Event('pause'));
    setHidden(true);
    await settle();
    setHidden(false);
    await settle();
    assert.equal(heads(), 1, 'still paused, still asleep');
});

test('a live run started on a paused video sleeps after its first answer', async (t) => {
    // The events are only half the state. A media element that has never
    // played fires no `pause`, so nothing would ever put this run to sleep
    // — and since the cap for a live source is now an inactivity cap, that
    // run would hold a transcoder session for the length of the film with
    // nobody watching. The two entry states are this one (autoplay blocked,
    // or the mount-time restore of a saved AI track) and the hidden tab
    // below.
    //
    // The first HEAD still goes out: it is what says whether the source is
    // live at all, and that is the only source a sleeping run saves money
    // on (see the non-live case below).
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? liveProgressResponse('3/3')
        : { ok: true, status: 200, json: async () => ({}) }));
    const heads = () => p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;
    assert.equal(p.video.paused, true, 'a video that has never played');

    const ai = p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]');
    click(ai);
    await settle();

    assert.equal(heads(), 1, 'one HEAD, to learn what the source is');
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(heads(), 1, 'and then nothing: nobody is watching a live run');
    // Asleep, not refused: the run was started and counted, and the chip
    // carries the count that first answer brought.
    assert.equal(p.events.filter((e) => e.name === 'subtitle-translate-start').length, 1);
    assert.equal(ai.querySelector('.tr-progress').hidden, false);
    assert.equal(ai.querySelector('.tr-progress').textContent, '· 3');
    assert.deepEqual(p.events.filter((e) => e.name === 'subtitle-translate-error'), [],
        'sleeping is not a failure');

    p.video.paused = false;
    p.video.dispatchEvent(new dom.window.Event('play'));
    await settle();
    assert.equal(heads(), 2, 'play wakes the same run');
});

test('a cached batch run on a paused video is not suspended at all', async (t) => {
    // The negative control for the rule above, and the reason it moved:
    // the guard used to run before the first HEAD, so it could not know the
    // source. An OpenSubtitles translation is a cached batch job of
    // seconds with no transcoder session behind it — and pausing the film
    // to open the picker is exactly when a viewer starts one. Suspending it
    // left them looking at `· 0%` and a spinner that never moved until they
    // pressed play.
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? progressResponse('12/400')
        : { ok: true, status: 200, json: async () => ({}) }));
    const heads = () => p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;
    assert.equal(p.video.paused, true, 'a video that has never played');

    const ai = p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]');
    click(ai);
    await settle();
    assert.equal(ai.querySelector('.tr-progress').textContent, '· 3%');

    // No X-Subtitle-Live in the answer, so the run carries on without a
    // press of play.
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(heads() > 1, `a batch run keeps polling while paused, got ${heads()} HEADs`);
});

test('a live run started in a hidden tab sleeps after its first answer', async (t) => {
    t.after(() => { destroyPlayer(); delete document.hidden; });
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? liveProgressResponse('3/3')
        : { ok: true, status: 200, json: async () => ({}) }));
    const heads = () => p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;

    // Hidden before the run starts — a background tab fires no
    // visibilitychange of its own, so again only the initial read can see
    // it. The video is playing, to isolate this from the paused case.
    p.video.paused = false;
    setHidden(true);
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();
    assert.equal(heads(), 1, 'one HEAD, to learn what the source is');
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(heads(), 1, 'a background tab pays for nothing after that');

    setHidden(false);
    await settle();
    assert.equal(heads(), 2, 'and it wakes when the tab comes forward');
});

// ---- one error per run ------------------------------------------------

test('an HTTP error and a late <track> error are one run, and one event', async (t) => {
    // fail() clears the refs and reports, but it used not to invalidate the
    // run. A <track> whose src this run set keeps loading after the poll
    // has already failed, and when it errors a moment later onTrackError's
    // identity check still passed — so code:'track' landed as a second
    // subtitle-translate-error for the same translation.
    // trackErrorReported only guards repeats of the track error itself.
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    let headCount = 0;
    p.setResponse((url, params) => {
        if (params && params.method === 'HEAD') {
            headCount++;
            // The first answer is real, so the run reloads the <track> and
            // wires its error handler; the next one fails the run.
            return headCount === 1 ? progressResponse('12/400') : { ok: false, status: 500, headers: { get: () => null } };
        }
        return { ok: true, status: 200, json: async () => ({}) };
    });

    p.video.paused = false;
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();
    const track = p.video.querySelector('track#tr-pt');
    assert.ok(track, 'the run must have a <track> to fail');
    assert.match(track.getAttribute('src'), /rev=12/, 'and it must have been pointed at a revision');

    await wait(POLL_INTERVAL_WINDOW_MS);
    const errors = () => p.events.filter((e) => e.name === 'subtitle-translate-error');
    assert.deepEqual(errors().map((e) => e.data.code), [500], 'the HTTP error is reported once');

    // The revision the dead run asked for now fails to load. It belongs to
    // nobody.
    track.dispatchEvent(new dom.window.Event('error'));
    await settle();
    assert.deepEqual(errors().map((e) => e.data.code), [500],
        'a late track error of a finished run must not be counted again');
});

// ---- the subtitle invariant -------------------------------------------
//
// Two renderers can draw subtitles on the same element: hls.js, from the
// transcoder's manifest, and the element's own <track>s. The picker is the
// one thing that says which — and hls.js does not know that. It re-scans
// the element's TextTracks on every `change` event and adopts a track of
// its own whenever it finds one that is not 'disabled', which is how a
// stage viewer ended up with an embedded Russian track drawn over the AI
// Catalan one they had chosen. These four tests are the invariant:
// side-loaded means hls.js is off and every track of its is disabled, and
// it is re-asserted after each transition of hls.js rather than written
// once.

test('choosing a side-loaded track disables hls.js’s own tracks — it never hides them', async () => {
    const p = mount({ hlsTracks: [['Full (rus)', 'showing'], ['Full (eng)', 'hidden']] });
    const hls = p.installHls({ subtitleTrack: 0, subtitleDisplay: true });
    p.wire();

    click(p.chip('os-os-ru'));
    await flush();

    assert.equal(hls.subtitleTrack, -1, 'hls.js is off the manifest track');
    assert.equal(hls.subtitleDisplay, false);
    assert.equal(p.mode('os-os-ru'), 'showing');
    assert.deepEqual(p.hlsModes(), ['disabled', 'disabled']);
    // The specific mode is the whole point: 'hidden' is what
    // onTextTracksChanged remembers and hands back to setSubtitleTrack, so
    // "off" written as 'hidden' is an instruction to turn it on again.
    assert.equal(p.hlsModes().includes('hidden'), false);
});

test('a translation saved in an earlier session survives hls.js arriving after it', async (t) => {
    // The measured race: activateSubtitle runs inside the mount, before
    // useHls has created the instance, so the hls.js half of the selection
    // cannot be written there — window.hlsPlayer is still null. It used to
    // be skipped entirely, leaving subtitleDisplay at hls.js's own default
    // of true (measured on the page: hlsDisp true, hlsSub -1, nine seconds
    // in) with nothing but a mode write needed to make it draw something.
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => {
        it.setResponse((url, params) => (params && params.method === 'HEAD'
            ? progressResponse('400/400')
            : { ok: true, status: 200, json: async () => ({}) }));
        const ai = it.chip('tr-pt');
        ai.setAttribute('data-saved', 'true');
        ai.setAttribute('data-default', 'true');
        ai.removeAttribute('data-offered');
        it.chip('none').removeAttribute('data-default');
        it.modal.setAttribute('data-subtitles-off', 'false');
    }, { hlsTracks: [['Full (rus)', 'showing']] });

    assert.equal(window.hlsPlayer, null, 'the restore ran with no HLS instance to write to');
    assert.equal(p.mode('tr-pt'), 'showing', 'the element half landed anyway');

    // canplay, and with it the instance.
    const hls = makeHls({ video: p.video, subtitleTrack: -1, subtitleDisplay: true });
    initDefaultTracks(hls, p.video);

    assert.equal(hls.subtitleDisplay, false, 'hls.js is told the viewer is not watching its tracks');
    assert.equal(hls.subtitleTrack, -1);
    assert.deepEqual(p.hlsModes(), ['disabled']);
    assert.equal(p.mode('tr-pt'), 'showing');
});

test('an hls.js track that latches on mid-playback is put back', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => {
        it.setResponse((url, params) => (params && params.method === 'HEAD'
            ? progressResponse('400/400')
            : { ok: true, status: 200, json: async () => ({}) }));
        it.installHls({ subtitleTrack: -1, subtitleDisplay: true });
    }, { hlsTracks: [['Full (rus)', 'hidden']] });
    const hls = window.hlsPlayer;

    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();
    assert.equal(p.mode('tr-pt'), 'showing');
    assert.deepEqual(p.hlsModes(), ['disabled']);

    // Bounded, and this is the assertion that catches the recursion. The
    // write of -1 disables the very track the same apply is about to show
    // and fires SUBTITLE_TRACK_SWITCH from inside itself, so the listener
    // runs nested at the one moment the selection cannot hold. Without the
    // re-entrancy guard in applySubtitleSelection this is hundreds of
    // writes and a blown stack; with it, one per apply.
    const trackWrites = () => hls.writes.filter(([k]) => k === 'subtitleTrack').length;
    assert.ok(trackWrites() <= 2, `one selection must not storm hls.js: ${trackWrites()} writes`);

    // Now hls.js does what it does: something wakes onTextTracksChanged, it
    // adopts a manifest track and starts drawing it. This is the state that
    // was measured on stage, reproduced by hand — the fake does not
    // implement hls.js's own reactions.
    p.hlsTrack(0).mode = 'showing';
    hls.emit(Hls.Events.SUBTITLE_TRACK_SWITCH, { id: 0 });
    await flush();

    assert.deepEqual(p.hlsModes(), ['disabled'], 'the manifest track is taken off screen again');
    assert.equal(hls.subtitleTrack, -1);
    assert.equal(hls.subtitleDisplay, false);
    assert.equal(p.mode('tr-pt'), 'showing', 'and the chosen one is still the one playing');

    // Terminating, not looping: the re-assertion's own writes come back as
    // more hls.js events, and a second pass over a state that already
    // agrees must write nothing.
    const writes = hls.writes.length;
    hls.emit(Hls.Events.SUBTITLE_TRACK_SWITCH, { id: -1 });
    hls.emit(Hls.Events.SUBTITLE_TRACKS_UPDATED, {});
    await flush();
    assert.equal(hls.writes.length, writes);
});

test('the re-assertion comes off with the player', async () => {
    const p = await mountPlayer((it) => it.installHls({ subtitleTrack: -1 }));
    const hls = window.hlsPlayer;
    assert.equal(hls.listenerCount(Hls.Events.SUBTITLE_TRACK_SWITCH), 1);
    destroyPlayer();
    assert.equal(hls.listenerCount(Hls.Events.SUBTITLE_TRACK_SWITCH), 0);
    assert.equal(hls.listenerCount(Hls.Events.SUBTITLE_TRACKS_UPDATED), 0);
});

// ---- the session seek --------------------------------------------------

test('a seek re-applies the picker’s current answer, not the one it started with', async (t) => {
    // The subtitles dialog stays open and clickable while a seek is in
    // flight (the POST, the manifest reload and the buffering after it are
    // seconds), so the selection that was playing when the seek began is
    // not necessarily the one to come back to. It used to be restored from
    // a snapshot taken at seek start — which, after a switch to a
    // side-loaded track, put hls.js back on the embedded track and left
    // both on screen.
    //
    // Through a real mount, because the seeker is not the only listener on
    // SUBTITLE_TRACKS_UPDATED: the player's re-assertion is registered
    // first and runs first, so this is also the path where the seeker
    // re-enters applySubtitleSelection through somebody else's listener.
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => {
        it.installHls({ subtitleTrack: 0, subtitleDisplay: true });
        // Playing at seek time: the embedded Russian track.
        it.chip('none').removeAttribute('data-default');
        it.chip('mp-0').setAttribute('data-default', 'true');
        it.modal.setAttribute('data-subtitles-off', 'false');
    }, { hlsTracks: [['Full (rus)', 'showing']] });
    const hls = window.hlsPlayer;

    const seeker = createSessionSeeker({
        hls,
        videoEl: p.video,
        sessionSeekUrl: '/session/seek',
        sourceUrl: 'https://x.test/index.m3u8',
        trackContainer: p.container,
    });
    const seeking = seeker.seek(120);
    await flush();

    // Mid-seek, the viewer switches to a side-loaded track.
    click(p.chip('os-os-ru'));
    await flush();

    // loadSource: hls.js reprocesses the element, dropping its selection
    // and disabling the element-backed tracks, then announces the new
    // track list.
    p.video.textTracks.find((t) => t.id === 'os-os-ru').mode = 'disabled';
    hls.emit(Hls.Events.SUBTITLE_TRACKS_UPDATED, {});

    assert.equal(hls.subtitleTrack, -1, 'the snapshot’s embedded track is not restored');
    assert.equal(hls.subtitleDisplay, false);
    assert.equal(p.mode('os-os-ru'), 'showing');
    assert.deepEqual(p.hlsModes(), ['disabled']);

    // And the cue/mode snapshot restored on `playing` does not undo it
    // either: it is the cues that have to survive a seek, not the modes.
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await seeking;
    assert.equal(p.mode('os-os-ru'), 'showing');
    assert.deepEqual(p.hlsModes(), ['disabled']);
    assert.equal(hls.subtitleTrack, -1);
    assert.ok(hls.writes.filter(([k]) => k === 'subtitleTrack').length <= 4,
        'two applies (the player\u2019s and the seeker\u2019s) write once each, not once per nested callback');
});

// ---- a track a seek emptied comes back when it is picked -----------------
//
// Bug (2026-09-17, reproduced on stage): hls.js clears the cues of every text
// track on loadSource, the seek snapshot only holds the track that was on,
// and the browser never refetches a src it has loaded. An OpenSubtitles
// track picked after a seek sat at readyState 2, showing, with 0 cues.
test('a side-loaded track picked after a seek is fetched again; one left off is not', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => {
        it.installHls({ subtitleTrack: 0, subtitleDisplay: true });
        it.chip('none').removeAttribute('data-default');
        it.chip('mp-0').setAttribute('data-default', 'true');
        it.modal.setAttribute('data-subtitles-off', 'false');
    }, { tracks: [['os-os-ru', false], ['os-os-en', false]], hlsTracks: [['Full (rus)', 'showing']] });
    const hls = window.hlsPlayer;
    const ru = p.video.querySelector('track#os-os-ru');
    const en = p.video.querySelector('track#os-os-en');
    // Both were loaded before the seek, as the browser does with <track>s.
    for (const el of [ru, en]) Object.defineProperty(el, 'readyState', { configurable: true, value: 2 });

    const seeker = createSessionSeeker({
        hls,
        videoEl: p.video,
        sessionSeekUrl: '/session/seek',
        sourceUrl: 'https://x.test/index.m3u8',
        trackContainer: p.container,
    });
    const seeking = seeker.seek(120);
    await flush();
    hls.emit(Hls.Events.SUBTITLE_TRACKS_UPDATED, {});
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await seeking;

    click(p.chip('os-os-ru'));
    await flush();

    assert.equal(p.mode('os-os-ru'), 'showing');
    assert.match(ru.getAttribute('src'), /wt-rf=\d+$/, 'the emptied track the viewer picked is fetched again');
    assert.equal(en.getAttribute('src'), 'https://x.test/os-os-en.vtt', 'a track nobody picked costs no request');
});

test('a <track> added while the seek POST is in flight is refetched when picked', async (t) => {
    // Found in review: the elements to mark were collected before the POST,
    // so a track that loaded during it was wiped by loadSource and marked by
    // nobody.
    t.after(() => destroyPlayer());
    let releasePost;
    const p = await mountPlayer((it) => {
        it.installHls({ subtitleTrack: 0, subtitleDisplay: true });
        it.chip('none').removeAttribute('data-default');
        it.chip('mp-0').setAttribute('data-default', 'true');
        it.modal.setAttribute('data-subtitles-off', 'false');
    }, { hlsTracks: [['Full (rus)', 'showing']] });
    p.setResponse((url, params) => (params && params.method === 'POST' && String(url).includes('/session/seek')
        ? new Promise((resolve) => { releasePost = () => resolve({ ok: true, status: 200, json: async () => ({}) }); })
        : { ok: true, status: 200, json: async () => ({}) }));
    const hls = window.hlsPlayer;
    const seeker = createSessionSeeker({
        hls, videoEl: p.video, sessionSeekUrl: '/session/seek', sourceUrl: 'https://x.test/index.m3u8', trackContainer: p.container,
    });
    const seeking = seeker.seek(120);
    await flush();
    const el = document.createElement('track');
    el.id = 'os-os-en';
    el.setAttribute('src', 'https://x.test/os-os-en.vtt');
    Object.defineProperty(el, 'readyState', { configurable: true, value: 2 });
    p.video.appendChild(el);
    releasePost();
    await flush();
    hls.emit(Hls.Events.SUBTITLE_TRACKS_UPDATED, {});
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await seeking;

    click(p.chip('os-os-en'));
    await flush();
    assert.match(p.video.querySelector('track#os-os-en').getAttribute('src'), /wt-rf=\d+$/);
});

test('a seek whose play() is refused settles instead of latching', async (t) => {
    // Found in review (2026-09-18): the seek pauses the old run itself and
    // is unlocked only by `playing`. A rejected play() was swallowed, so
    // `playing` never came: isSeeking stayed true and every later seek
    // returned at once, for the rest of the session.
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => { it.installHls({}); });
    p.video.paused = false;
    let plays = 0;
    p.video.pause = () => { p.video.paused = true; };
    p.video.play = () => { plays++; return Promise.reject(new dom.window.DOMException('nope', 'NotAllowedError')); };
    const changes = [];
    const seeker = createSessionSeeker({
        hls: window.hlsPlayer, videoEl: p.video, sessionSeekUrl: '/session/seek',
        sourceUrl: 'https://x.test/index.m3u8', trackContainer: p.container,
        onSeekingChange: (v) => changes.push(v),
    });
    const seeking = seeker.seek(120);
    await flush();
    assert.equal(plays, 1);
    assert.equal(seeker.isSeeking(), true, 'one refusal is not the end: the reload may have aborted it');

    p.video.dispatchEvent(new dom.window.Event('canplay'));
    await flush();
    await seeking;
    assert.equal(plays, 2, 'tried once more when the new source can play');
    assert.equal(seeker.isSeeking(), false, 'refused twice: settled paused, seeking unlocked');
    assert.deepEqual(changes, [true, false]);
});

// ---- a seek kicks the translation poll ---------------------------------
//
// Bug (2026-09-16): with a live AI translation running, a seek jumps the
// video ahead of what the poll knows about, so the first cues for the new
// position waited out whatever was left of the 3 s poll interval and then
// TRACK_RELOAD_INTERVAL_MS (15 s) on top of that \u2014 on top of the service's
// own lag (fixed separately, server side). The fix is `handleSeek`'s
// `onSeekOffsetChange` calling `pollStopRef.current.kick()`: an immediate
// HEAD (subtitle-progress.js's kick()) and a mark on the change it finds
// that lets `reload()` bypass its own throttle once.
//
// Through `initPlayer` and a real keydown, not `createSessionSeeker`
// directly (see the seek test above): `kick()` lives inside the
// `onSeekOffsetChange` closure `handleSeek` builds, so only the mounted
// component's own seek path exercises it.
test('a session seek kicks the translation poll and unthrottles the next reload', async (t) => {
    t.after(() => destroyPlayer());
    const p = mount({ tracks: [['tr-pt', false]] });
    p.video.dataset.sessionId = 's1';
    p.video.dataset.sessionSeekUrl = '/session/seek';
    p.video.setAttribute('data-duration', '3600');
    let done = 5;
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? liveProgressResponse(`${done}/${done}`)
        : { ok: true, status: 200, json: async () => ({}) }));
    await initPlayer(p.container);
    await settle();

    const heads = () => p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;
    const ai = p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]');
    const track = () => p.video.querySelector('#tr-pt');

    p.video.paused = false;
    click(ai);
    await settle();
    assert.equal(heads(), 1, 'the run polls on start');
    assert.ok(track().getAttribute('src').includes('rev=5'), 'the cold-start reload is unthrottled on its own');

    // The service has more cues ready by the time the seek lands (its own
    // fix \u2014 translating the current run first \u2014 is separate from this
    // one).
    done = 9;

    // ArrowRight reaches the same handleSeek a drag on the timeline does,
    // well inside both the 3 s poll interval and the 15 s reload throttle
    // the first reload just started.
    document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await settle();

    assert.ok(heads() >= 2, `the seek must kick a HEAD at once, not wait for the next 3 s tick \u2014 got ${heads()} HEADs`);
    assert.ok(track().getAttribute('src').includes('rev=9'),
        'the reload for the new count must not wait out the 15 s throttle either');
});

// The element carries its movie-time offset (data-run-offset) for code
// outside the player -- the transfer status's grace window
// (lib/playerActivity.js inGrace) reads it on the restarting source's own
// events. Written when the seek's answer arrives, before the source is
// reloaded, not on the next render: the restart's emptied/loadstart/waiting
// otherwise read the new element clock (0) against the old offset.
test('a session seek puts its offset on the element before the source restarts', async (t) => {
    t.after(() => destroyPlayer());
    const p = mount();
    p.video.dataset.sessionId = 's1';
    p.video.dataset.sessionSeekUrl = '/session/seek';
    p.video.setAttribute('data-duration', '3600');
    p.setResponse((url, params) => ({
        ok: true, status: 200,
        // The seek's POST answers where the new run starts; the mount's
        // GET, that the first run starts at 0.
        json: async () => ({ offset: params && params.method === 'POST' ? 1470 : 0 }),
    }));
    await initPlayer(p.container);
    await settle();
    assert.equal(p.video.dataset.runOffset, '0');
    let atRestart = null;
    p.video.load = function load() { atRestart = this.dataset.runOffset; };
    if (window.hlsPlayer) {
        const orig = window.hlsPlayer.loadSource;
        window.hlsPlayer.loadSource = (...a) => { atRestart = p.video.dataset.runOffset; return orig && orig.apply(window.hlsPlayer, a); };
    }
    document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await settle();
    assert.equal(atRestart, '1470', 'the offset on the element when the source restarts');
    assert.equal(p.video.dataset.runOffset, '1470');
});

// The grace popup goes up as the film leaves the free window, and the
// element says so (data-grace-cta-shown): until it does, the transfer status
// counts the popup as on its way (lib/playerActivity.js graceOfferDue) and
// keeps its own plan box down -- the popup comes from a render effect, a
// frame or more after the element's clock crosses.
test('the grace popup, once up, is marked on the element', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((page) => {
        page.video.dataset.graceDurationSec = '30';
        const cta = document.createElement('div');
        cta.id = 'grace-cta';
        cta.className = 'hidden';
        cta.setAttribute('data-upsell-surface', 'grace');
        page.container.appendChild(cta);
    });
    await playPast(p, 29);
    assert.ok(document.getElementById('grace-cta').classList.contains('hidden'), 'inside the window: no popup');
    assert.equal('graceCtaShown' in p.video.dataset, false);
    await playPast(p, 31);
    assert.equal(document.getElementById('grace-cta').classList.contains('hidden'), false, 'past it: the popup');
    assert.equal('graceCtaShown' in p.video.dataset, true, 'and the element says it is up');
});

// The viewer's answer to the popup -- "continue at N Mbps" or its close --
// goes on the element too (data-grace-cta-answered): they have just been told
// of the cap, and the transfer status sells the way out again only at the
// player's first real stall (lib/playerActivity.js offerAnswered), not the
// moment the popup closes. The trial link is no answer: it opens a new tab
// and the popup stays.
for (const [button, answer] of [['.grace-cta-continue', 'continue'], ['.grace-cta-close', 'dismiss']]) {
    test(`the grace popup answered "${answer}" is marked on the element`, async (t) => {
        t.after(() => destroyPlayer());
        const p = await mountPlayer((page) => {
            page.video.dataset.graceDurationSec = '30';
            const cta = document.createElement('div');
            cta.id = 'grace-cta';
            cta.className = 'hidden';
            cta.setAttribute('data-upsell-surface', 'grace');
            cta.innerHTML = '<button type="button" class="grace-cta-close"></button>'
                + '<a class="btn" href="/trial?from=grace" target="_blank">trial</a>'
                + '<button type="button" class="grace-cta-continue"></button>';
            page.container.appendChild(cta);
        });
        await playPast(p, 31);
        const cta = document.getElementById('grace-cta');
        assert.equal(cta.classList.contains('hidden'), false, 'the popup is up');
        assert.equal('graceCtaAnswered' in p.video.dataset, false, 'shown is not answered');
        cta.querySelector('a').addEventListener('click', (e) => e.preventDefault());
        cta.querySelector('a').click();
        assert.equal('graceCtaAnswered' in p.video.dataset, false, 'the trial link is not an answer');
        assert.equal(cta.classList.contains('hidden'), false);
        cta.querySelector(button).click();
        assert.equal(cta.classList.contains('hidden'), true, 'closed');
        assert.equal(p.video.dataset.graceCtaAnswered, answer);
        assert.ok(p.events.some((e) => e.name === 'grace-soft-cta-click' && e.data.action === answer), 'Umami as before');
    });
}

// ---- the grace popup holds the film (grace-hold.js) ---------------------
//
// Owner, 2026-09-26: the film stops when the popup comes up, and goes on with
// the answer -- only if the popup stopped it. hls.js keeps filling the buffer
// meanwhile, and the transfer status keeps reading the player as playing
// (lib/playerActivity.js: the page's pause, data-grace-cta-hold, not the
// viewer's).

// gracePopup puts the popup on the page as stream_video.html renders it: the
// close, the trial link (a new tab), "continue at N Mbps".
function gracePopup(page, sec = 30) {
    page.video.dataset.graceDurationSec = String(sec);
    const cta = document.createElement('div');
    cta.id = 'grace-cta';
    cta.className = 'hidden';
    cta.setAttribute('data-upsell-surface', 'grace');
    cta.innerHTML = '<button type="button" class="grace-cta-close"></button>'
        + '<a class="btn" href="/trial?from=grace" target="_blank">trial</a>'
        + '<button type="button" class="grace-cta-continue"></button>';
    page.container.appendChild(cta);
}
const graceCta = () => document.getElementById('grace-cta');
const popupUp = () => !graceCta().classList.contains('hidden');
const graceClick = (p) => p.events.find((e) => e.name === 'grace-soft-cta-click');
const keydown = (key) => document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key, bubbles: true }));

for (const [button, answer] of [['.grace-cta-continue', 'continue'], ['.grace-cta-close', 'dismiss']]) {
    test(`the grace popup stops the film, and "${answer}" plays it again`, async (t) => {
        t.after(() => destroyPlayer());
        const p = await mountPlayer((page) => gracePopup(page));
        const log = playback(p.video);
        const activity = createPlayerActivity(document);
        t.after(() => activity.stop());
        await playPast(p, 31);
        assert.ok(popupUp(), 'the popup is up');
        assert.equal(p.video.paused, true, 'and the film stopped with it');
        assert.equal(log.pause, 1);
        assert.ok('graceCtaHold' in p.video.dataset, 'the page’s pause, on the element');
        assert.equal(activity.streaming(), true, 'the status keeps the viewer on the chain: not their pause');
        assert.equal(activity.state(), 'playing');
        assert.deepEqual(p.events.find((e) => e.name === 'grace-soft-cta-shown').data, { paused: true });
        await settle();
        assert.equal(p.video.paused, true, 'it stays stopped');

        graceCta().querySelector(button).click();
        await settle();
        assert.equal(popupUp(), false);
        assert.equal(p.video.dataset.graceCtaAnswered, answer);
        assert.equal(log.play, 1, 'the answer starts it');
        assert.equal(p.video.paused, false, 'and nothing pauses it again');
        assert.equal('graceCtaHold' in p.video.dataset, false);
        assert.deepEqual(graceClick(p).data, { action: answer, via: 'button', paused: true });
    });
}

// The popup comes up a frame after the clock crosses (a render effect): a
// viewer who paused in between paused it themselves, and the answer leaves
// their pause alone.
test('a film the viewer had paused stays paused after the answer', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((page) => gracePopup(page));
    const log = playback(p.video);
    p.video.paused = false;
    p.video.currentTime = 31;
    // After the player's clock has read 0:31 in this frame, before the
    // effect that puts the popup up runs in the next one.
    await new Promise((resolve) => requestAnimationFrame(() => { p.video.pause(); resolve(); }));
    await settle();
    assert.ok(popupUp(), 'fixture: the popup came up over a film the viewer paused');
    assert.equal(log.pause, 1, 'their pause, and no other');
    assert.equal('graceCtaHold' in p.video.dataset, false, 'not the page’s pause');
    assert.deepEqual(p.events.find((e) => e.name === 'grace-soft-cta-shown').data, { paused: false });

    graceCta().querySelector('.grace-cta-continue').click();
    await settle();
    assert.equal(popupUp(), false);
    assert.equal(p.video.dataset.graceCtaAnswered, 'continue');
    assert.equal(log.play, 0, 'nothing starts it');
    assert.equal(p.video.paused, true);
    assert.equal(graceClick(p).data.paused, false);
});

// The trial link opens a new tab and the popup stays: no answer, and the
// film does not start behind it.
test('the trial link starts nothing behind the popup', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((page) => gracePopup(page));
    const log = playback(p.video);
    await playPast(p, 31);
    const a = graceCta().querySelector('a');
    a.addEventListener('click', (e) => e.preventDefault());
    a.click();
    await settle();
    assert.ok(popupUp());
    assert.equal(p.video.paused, true);
    assert.equal(log.play, 0);
    assert.ok('graceCtaHold' in p.video.dataset);
    assert.equal('graceCtaAnswered' in p.video.dataset, false);
    assert.equal(graceClick(p), undefined);
});

// Play while the popup is up (space here; the big button, a click on the
// picture and the headset go through the same toggle) is the answer
// "continue": it says what the button says, and a Play that did nothing
// would read as a broken player. It plays, whoever paused the film.
test('Play while the popup is up is the answer "continue"', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((page) => gracePopup(page));
    const log = playback(p.video);
    await playPast(p, 31);
    assert.equal(p.video.paused, true, 'fixture: held');
    keydown(' ');
    await settle();
    assert.equal(popupUp(), false, 'answered');
    assert.equal(p.video.dataset.graceCtaAnswered, 'continue');
    assert.equal(log.play, 1);
    assert.equal(p.video.paused, false);
    assert.deepEqual(graceClick(p).data, { action: 'continue', via: 'play', paused: true });
    keydown(' ');
    await settle();
    assert.equal(p.video.paused, true, 'from here on space is play/pause as ever');
    assert.equal(p.events.filter((e) => e.name === 'grace-soft-cta-click').length, 1, 'one answer');
});

// Whatever else starts the film while the popup is up -- the embed's
// player_play here; the subtitle catch-up letting go is another -- is put
// back on its `play` event, and counted as playback the answer resumes.
test('whatever else starts the film behind the popup is put back until the answer', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((page) => gracePopup(page));
    const log = playback(p.video);
    await playPast(p, 31);
    window.dispatchEvent(new dom.window.CustomEvent('player_play'));
    await settle();
    assert.equal(log.play, 1, 'fixture: something played it');
    assert.equal(p.video.paused, true, 'put back');
    graceCta().querySelector('.grace-cta-close').click();
    await settle();
    assert.equal(p.video.paused, false, 'the answer starts it');
});

test('in fullscreen the popup leaves it, and the film stops', async (t) => {
    let exited = 0;
    Object.defineProperty(document, 'fullscreenElement', { configurable: true, get: () => document.body });
    document.exitFullscreen = () => { exited++; return Promise.resolve(); };
    t.after(() => {
        destroyPlayer();
        delete document.fullscreenElement;
        delete document.exitFullscreen;
    });
    const p = await mountPlayer((page) => gracePopup(page));
    playback(p.video);
    await playPast(p, 31);
    assert.equal(exited, 1, 'out of fullscreen: the popup lives outside it');
    assert.ok(popupUp());
    assert.equal(p.video.paused, true);
});

// A session seek past the window: the timeline shows the target at once, so
// the popup comes up as the seek starts, and the seek's play() -- when the
// transcoder's new run is ready -- would start the film behind it. The seek
// asks the hold first (session-seek.js holdPlayback), loads the new run
// without playing it, and lets go when it can play; the answer starts it.
test('a session seek past the window: the new run waits for the answer', async (t) => {
    t.after(() => destroyPlayer());
    const p = mount();
    gracePopup(p, 10);
    p.video.dataset.sessionId = 's1';
    p.video.dataset.sessionSeekUrl = '/session/seek';
    p.video.setAttribute('data-duration', '3600');
    let releasePost = null;
    p.setResponse((url, params) => {
        if (params && params.method === 'POST' && String(url).startsWith('/session/seek')) {
            return new Promise((resolve) => {
                releasePost = () => resolve({ ok: true, status: 200, json: async () => ({ offset: 15 }) });
            });
        }
        return { ok: true, status: 200, headers: new dom.window.Headers(), json: async () => ({ offset: 0 }) };
    });
    await initPlayer(p.container);
    await settle();
    const log = playback(p.video);
    p.video.load = () => {};
    const posts = () => p.calls.filter((c) => c.params && c.params.method === 'POST' && String(c.url).startsWith('/session/seek')).length;
    p.video.paused = false;
    keydown('ArrowRight'); // 0:00 -> 0:15, past the 10 s window
    await settle();
    assert.ok(popupUp(), 'the popup came up with the seek');
    assert.equal(p.video.paused, true, 'the film stopped');
    // The transcoder answers: the new run is loaded -- and not played.
    releasePost();
    await settle();
    assert.equal(log.play, 0, 'the seek did not start the film behind the popup');
    assert.equal(p.video.paused, true);
    assert.ok('graceCtaHold' in p.video.dataset);
    keydown('ArrowRight');
    await settle();
    assert.equal(posts(), 1, 'still seeking: nothing else is sought yet');
    // The new run can play: the seek lets go, paused under the popup.
    p.video.dispatchEvent(new dom.window.Event('canplay'));
    await settle();
    assert.equal(log.play, 0, 'still not played');
    // ...and the element's `autoplay`, which the reload re-arms, starts it by
    // itself (Chrome, live, 2026-09-26): put back.
    p.video.play();
    await settle();
    assert.equal(p.video.paused, true, 'autoplay put back');
    graceCta().querySelector('.grace-cta-continue').click();
    await settle();
    assert.equal(log.play, 2, 'the answer starts it');
    assert.equal(p.video.paused, false);
    assert.equal(graceClick(p).data.paused, true);
    keydown('ArrowRight');
    await settle();
    assert.equal(posts(), 2, 'the held seek had let go: seeking works again');
});

// The same through hls.js (the seeker pauses the old run itself there, and
// starts the new one only if the film was playing): a seek that lands paused
// never asks the hold, and the answer leaves the film paused.
test('a held seek through hls.js settles paused on canplay; one that lands paused stays paused', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => { it.installHls({}); });
    const log = playback(p.video);
    const hold = createGraceHold(p.video);
    const seeker = createSessionSeeker({
        hls: window.hlsPlayer, videoEl: p.video, sessionSeekUrl: '/session/seek',
        sourceUrl: 'https://x.test/index.m3u8', trackContainer: p.container,
        holdPlayback: () => hold.holds(),
    });
    p.video.paused = false;
    const seeking = seeker.seek(120);
    hold.start(); // the popup, up as the seek starts
    await flush();
    await flush();
    assert.equal(log.play, 0, 'loaded, not played');
    assert.equal(seeker.isSeeking(), true);
    p.video.dispatchEvent(new dom.window.Event('canplay'));
    await flush();
    assert.equal(seeker.isSeeking(), false, 'settled when it could play');
    await seeking;
    assert.equal(p.video.paused, true);
    assert.equal(hold.release(), true);
    assert.equal(p.video.paused, false, 'the answer starts it');
    await settle();

    p.video.pause();
    const again = createGraceHold(p.video);
    again.start();
    seeker.seek(600);
    await flush();
    await flush();
    assert.equal(again.held(), false, 'a seek that lands paused does not ask');
    assert.equal(again.release(), false);
    assert.equal(p.video.paused, true, 'and stays paused');
});

// The popup comes up after the seek's play() went out (the hold's pause
// aborts it, and the seek retries at canplay): the retry asks the hold, and
// the seek settles paused instead of playing behind it.
test('a seek whose play() the popup aborted settles paused at canplay', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => { it.installHls({}); });
    let plays = 0;
    p.video.pause = () => { p.video.paused = true; };
    p.video.play = () => { plays++; return Promise.reject(new dom.window.DOMException('interrupted by a call to pause()', 'AbortError')); };
    const hold = createGraceHold(p.video);
    const seeker = createSessionSeeker({
        hls: window.hlsPlayer, videoEl: p.video, sessionSeekUrl: '/session/seek',
        sourceUrl: 'https://x.test/index.m3u8', trackContainer: p.container,
        holdPlayback: () => hold.holds(),
    });
    p.video.paused = false;
    const seeking = seeker.seek(120);
    await flush();
    await flush();
    assert.equal(plays, 1, 'fixture: the new run was started before the popup');
    hold.start();
    p.video.dispatchEvent(new dom.window.Event('canplay'));
    await flush();
    assert.equal(plays, 1, 'the retry asked the hold and did not play');
    assert.equal(seeker.isSeeking(), false, 'settled');
    await seeking;
    assert.equal(hold.held(), true, 'the answer will start it');
});

// A refused seek restarts the old run it paused -- not behind the popup.
test('a refused seek does not restart the film behind the popup', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => { it.installHls({}); });
    const log = playback(p.video);
    let refuse = null;
    p.setResponse(() => new Promise((resolve) => { refuse = () => resolve({ ok: false, status: 503 }); }));
    const hold = createGraceHold(p.video);
    const seeker = createSessionSeeker({
        hls: window.hlsPlayer, videoEl: p.video, sessionSeekUrl: '/session/seek',
        sourceUrl: 'https://x.test/index.m3u8', trackContainer: p.container,
        holdPlayback: () => hold.holds(),
    });
    p.video.paused = false;
    const seeking = seeker.seek(120); // pauses the playing film for the POST
    hold.start(); // the popup, up while the POST is out
    await flush();
    refuse();
    await seeking;
    assert.equal(seeker.isSeeking(), false);
    assert.equal(log.play, 0, 'not restarted behind the popup');
    assert.equal(p.video.paused, true);
    assert.equal(hold.held(), true, 'the answer restarts it');
});

// The player goes while the popup holds the film (a teardown; "Next" below):
// the answer given after that starts nothing, and the next player is free.
test('the player goes while the popup holds: nothing resumes, the next one starts free', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((page) => gracePopup(page));
    const log = playback(p.video);
    await playPast(p, 31);
    assert.equal(p.video.paused, true, 'fixture: held');
    const old = p.video;
    destroyPlayer();
    assert.equal('graceCtaHold' in old.dataset, false);
    graceCta().querySelector('.grace-cta-continue').click();
    await settle();
    assert.equal(log.play, 0, 'the film that went does not start');

    const q = await mountPlayer((page) => gracePopup(page));
    const qlog = playback(q.video);
    await playPast(q, 10);
    q.video.pause();
    q.video.play();
    await settle();
    assert.equal(qlog.pause, 1);
    assert.equal(q.video.paused, false, 'nothing holds the new film inside its window');
});

test('Next while the popup holds: the answer does not start the film being left', async (t) => {
    t.after(() => { destroyPlayer(); window.sessionStorage.clear(); });
    const p = await mountPlayer((page) => {
        gracePopup(page);
        Object.assign(page.video.dataset, { nextItemId: 'i2', nextPath: 'S01/e02.mkv', nextKind: 'episode', nextLabel: 'E02' });
        // What canMoveOn looks for: the start form and #content.
        const form = document.createElement('form');
        form.setAttribute('action', 'https://webtor.io/stream-video');
        form.innerHTML = '<input type="hidden" name="resource-id" value="res"><input type="hidden" name="item-id" value="i1">';
        page.container.appendChild(form);
        const content = document.createElement('div');
        content.id = 'content';
        page.container.appendChild(content);
    });
    const log = playback(p.video);
    await playPast(p, 31);
    assert.equal(p.video.paused, true, 'fixture: held');
    keydown('n');
    await settle();
    graceCta().querySelector('.grace-cta-continue').click();
    await settle();
    assert.equal(log.play, 0, 'the viewer said next');
    assert.equal(p.video.paused, true);
});

test('kick() is a no-op with no translation running \u2014 a seek with nothing playing does not throw', async (t) => {
    t.after(() => destroyPlayer());
    const p = mount();
    p.video.dataset.sessionId = 's1';
    p.video.dataset.sessionSeekUrl = '/session/seek';
    p.video.setAttribute('data-duration', '3600');
    p.setResponse(() => ({ ok: true, status: 200, json: async () => ({}) }));
    await initPlayer(p.container);
    await settle();

    document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await settle();
    // Nothing to assert beyond "it didn't throw" \u2014 pollStopRef.current is
    // null, and kickTranslationPoll's own guard is the whole of the fix
    // for that case.
});

// ---- the catching-up banner -------------------------------------------
//
// A live (embedded-track) translation runs alongside the transcode and can
// fall behind the playhead: the film plays on and the cues for what is on
// screen have not been written yet. The service says where its frontier is
// (X-Subtitle-Pending-From, movie time), the player compares it with the
// playhead, and the banner offers to wait.
//
// The i18n bundle is empty under `node --test` (see jsx-hooks.mjs), so t()
// answers with the key — which is exactly what makes "which of the two
// sentences is showing" assertable.

// catchUpResponse is a live answer that also carries the frontier.
// `pendingFrom` null is an older service, or a run with nothing pending
// ahead — the two cases that must produce no banner at all.
const catchUpResponse = (header, pendingFrom) => ({
    ok: true,
    status: 200,
    headers: {
        get: (n) => {
            if (n === 'X-Subtitle-Progress') return header;
            if (n === 'X-Subtitle-Live') return '1';
            if (n === 'X-Subtitle-Pending-From') return pendingFrom;
            return null;
        },
    },
    json: async () => ({}),
});

// jsdom implements neither play() nor pause() (they are the "not
// implemented" stubs, and pause() fires no event). These are what a
// browser does as far as this feature can tell: the flag flips at once
// and the event is *queued* — HTML queues a media element task for
// `pause` and `play` rather than dispatching inside the call.
//
// The asynchrony is not decoration. handleWait pauses and then resumes
// the poll, so a synchronously dispatched `pause` would land before the
// resume and be undone by it — which hides the one thing the
// waitingRef guard in sleep() is there for: in a browser the event
// arrives after the resume, and without the guard it would put the run
// to sleep for good, leaving the viewer paused on a translation nobody
// is asking about any more.
function playback(video) {
    const log = { play: 0, pause: 0 };
    const queue = (name) => setTimeout(() => video.dispatchEvent(new dom.window.Event(name)), 0);
    video.play = () => {
        log.play++;
        video.paused = false;
        queue('play');
        return Promise.resolve();
    };
    video.pause = () => {
        log.pause++;
        video.paused = true;
        queue('pause');
    };
    return log;
}

const catchUpBanner = (p) => p.container.querySelector('.wt-catchup');
// The pill that says "behind" -- as opposed to the few seconds of "caught up"
// that follow it since 2026-09-18 (doneBanner). "Nothing says behind" is what
// the assertions below mean by null.
const behindBanner = (p) => p.container.querySelector('.wt-catchup:not(.wt-catchup--done)');
const doneBanner = (p) => p.container.querySelector('.wt-catchup--done');

// pickPausedThenPlay starts the run the way a viewer does who chooses the
// track before pressing play. Since 2026-09-18 a run started over a PLAYING
// film opens the hold window (see "starting a translation over a playing
// film" below); the tests about the passive banner, the manual Wait and the
// run mismatch are about a run that is simply behind, so they start here.
async function pickPausedThenPlay(p, at) {
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();
    p.video.paused = false;
    p.video.currentTime = at;
    p.video.dispatchEvent(new dom.window.Event('play'));
    await settle();
}
const catchUpText = (p) => catchUpBanner(p).querySelector('.wt-catchup-text').textContent;

test('a translation at the playhead holds the film; one that gets ahead lets it go', async (t) => {
    // Owner, 2026-09-18: the pill used to OFFER to wait here. The player now
    // holds the film by itself whenever the translation is behind.
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    const log = playback(p.video);
    // The frontier — where the untranslated part starts — is right where
    // the viewer is, so the next thing they hear has no subtitle yet.
    let pending = '100';
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? catchUpResponse('12/400', pending)
        : { ok: true, status: 200, json: async () => ({}) }));

    await pickPausedThenPlay(p, 100);

    assert.ok(catchUpBanner(p), 'the run is at the playhead, so it is behind it');
    assert.equal(catchUpText(p), 'player.subtitleCatchUpWaitingShort');
    assert.equal(p.video.paused, true, 'and the film is held for it');
    assert.equal(p.events.filter((e) => e.name === 'subtitle-translate-wait').at(-1).data.auto, true);
    // No number, on any source (2026-09-18): on a live one `total - done` is
    // everything the transcoder has emitted minutes ahead of the viewer --
    // 388 here, ~300 on the owner's screen -- and reads as "never".
    assert.equal(catchUpBanner(p).dataset.remaining, undefined, 'no cue count on the pill');
    assert.equal(catchUpBanner(p).querySelector('.wt-catchup-text-short'), null, 'one copy, not a long and a short one');
    assert.ok(catchUpBanner(p).querySelector('.wt-catchup-btn'), 'with Keep watching on it');

    // The service gets comfortably ahead (past the 5 s clear margin).
    pending = '400';
    const playsBefore = log.play;
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(behindBanner(p), null, 'nothing to catch up to, nothing to say');
    assert.ok(log.play > playsBefore, 'and the film goes on by itself');
});

test('a service that sends no frontier shows no banner at all', async (t) => {
    // The compatibility case, and the one the client cannot get wrong:
    // every deployment before the header shipped, every batch source, and
    // every live run with nothing pending ahead. Absence is "no banner",
    // never "behind".
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? liveProgressResponse('12/400')
        : { ok: true, status: 200, json: async () => ({}) }));

    p.video.paused = false;
    p.video.currentTime = 100;
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();

    assert.equal(behindBanner(p), null);
    assert.equal(p.container.querySelector('.tr-progress').hidden, false, 'the chip still reports the run');
});

test('Wait pauses the film, keeps the poll awake, and plays again once the run is ahead', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    // Eight seconds behind the playhead.
    let pending = '92';
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? catchUpResponse('12/400', pending)
        : { ok: true, status: 200, json: async () => ({}) }));
    const heads = () => p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;
    const log = playback(p.video);

    await pickPausedThenPlay(p, 100);
    assert.ok(catchUpBanner(p));
    // The Wait button is what the pill offers a viewer who overruled the
    // automatic hold: Keep watching first.
    click(catchUpBanner(p).querySelector('.wt-catchup-btn'));
    await settle();
    assert.equal(p.video.paused, false, 'fixture: Keep watching plays the film');
    assert.equal(catchUpText(p), 'player.subtitleCatchUpShort', 'fixture: and the pill goes back to offering');
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(p.video.paused, false, 'an overruled hold does not come back on the next tick');
    const pausesBefore = log.pause;

    click(catchUpBanner(p).querySelector('.wt-catchup-btn'));
    await settle();

    assert.equal(log.pause, pausesBefore + 1, 'Wait pauses the film');
    assert.equal(p.video.paused, true);
    assert.equal(catchUpText(p), 'player.subtitleCatchUpWaitingShort', 'and the banner says why it is paused');
    const waits = p.events.filter((e) => e.name === 'subtitle-translate-wait');
    assert.equal(waits.length, 2, 'the automatic hold, then one event for one press');
    assert.equal(waits[1].data.auto, false);
    assert.equal(waits[1].data.lang, 'pt');
    assert.equal(waits[1].data.behind, 8, 'how far behind the run was when they pressed it');

    // The whole of the feature: a pause normally suspends the poll, and
    // this one must not — the HEAD every 3 s is what the viewer is waiting
    // on, and for a live source it is what keeps the transcoder session
    // (and the translation reading it) alive.
    const atPause = heads();
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(heads() > atPause, `the poll must stay awake while waiting: ${heads()} HEADs vs ${atPause} at the pause`);
    assert.ok(catchUpBanner(p), 'and the banner stands until the run catches up');

    // Comfortably ahead: the film goes back on by itself.
    pending = '400';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(log.play >= 1, 'playback resumes on its own');
    assert.equal(behindBanner(p), null);
    const dones = p.events.filter((e) => e.name === 'subtitle-translate-wait-done');
    assert.equal(dones.length, 1, 'one event for one wait');
    assert.equal(dones[0].data.lang, 'pt');
    assert.ok(typeof dones[0].data.seconds === 'number' && dones[0].data.seconds >= 0,
        `the wait is measured: ${JSON.stringify(dones[0].data)}`);
    // 8 s of film went by without subtitles: back to that line plus the
    // lead-in (8 + 2), inside the playlist that is playing.
    assert.equal(dones[0].data.rewind, 10);
    assert.equal(p.video.currentTime, 90, 'the film is put back to the line the viewer missed');
});

test('× takes the banner away and the next trailing tick does not bring it back', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    let pending = '100';
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? catchUpResponse('12/400', pending)
        : { ok: true, status: 200, json: async () => ({}) }));

    p.video.paused = false;
    p.video.currentTime = 100;
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();
    assert.ok(catchUpBanner(p));

    click(catchUpBanner(p).querySelector('.wt-catchup-close'));
    await settle();
    assert.equal(behindBanner(p), null, '× hides it');

    // Still behind, still dismissed: a banner that came back three
    // seconds later would make the × read as broken.
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(behindBanner(p), null, 'and it stays hidden while the run is still behind');

    // The dismissal was about a stretch of film the run was behind on.
    // Once it is no longer behind, that stretch is over...
    pending = '400';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(behindBanner(p), null, 'caught up: nothing to show either way');

    // ...and the next time it falls behind, it may say so again.
    pending = '100';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(catchUpBanner(p), 'a fresh stretch of film gets a fresh offer');
});

// ---- a seek waits for its subtitles -------------------------------------
//
// After a seek into film the live translation has not reached, the first
// lines play with no subtitles. The seek already costs the viewer a pause
// (the transcoder restarts), so the wait for those lines is folded into it:
// the same wait as the Wait button, started by the seek, and bounded — one
// slow batch must not turn a seek into a hang.

// mountSessionRun mounts a transcoder-session player with a live AI run on
// the Portuguese track, playing, and returns what the tests drive.
async function mountSessionRun(t, respond, { sessionOffset = 0, startPaused = false } = {}) {
    t.after(() => destroyPlayer());
    const p = mount({ tracks: [['tr-pt', false]] });
    p.video.dataset.sessionId = 's1';
    p.video.dataset.sessionSeekUrl = '/session/seek';
    p.video.setAttribute('data-duration', '3600');
    // The GET of the seek URL at mount is how the player learns the offset
    // of the session it joined (a resume lands mid-film); the seek POST
    // answers ok without an offset, like a transcoder from before it said
    // (the fallback quantization is what these tests pin).
    p.setResponse((url, params) => {
        if (params && params.method === 'HEAD') return respond();
        if (params && params.method === 'POST') return { ok: true, status: 200, json: async () => ({ ok: true }) };
        return { ok: true, status: 200, json: async () => ({ offset: sessionOffset }) };
    });
    await initPlayer(p.container);
    await settle();
    const log = playback(p.video);
    if (startPaused) {
        await pickPausedThenPlay(p, 1);
    } else {
        p.video.paused = false;
        p.video.currentTime = 1;
        click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
        await settle();
    }
    // ArrowRight is the same handleSeek a drag on the timeline reaches.
    const seek = async () => {
        document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
        await settle();
        // The new run is playing: what a browser reports once the seek lands.
        p.video.currentTime = 1;
        p.video.dispatchEvent(new dom.window.Event('playing'));
        await settle();
    };
    const events = (name) => p.events.filter((e) => e.name === name);
    return { p, log, seek, events };
}

test('a seek into untranslated film holds playback until the translation is ahead, then plays', async (t) => {
    let pending = '400';
    const { p, log, seek, events } = await mountSessionRun(t, () => catchUpResponse('12/400', pending));
    assert.equal(behindBanner(p), null, 'ahead before the seek: nothing to say');

    // The seek lands on film the run has not translated.
    pending = '0';
    const pausesBefore = log.pause;
    await seek();

    assert.ok(log.pause > pausesBefore, 'the seek holds playback for its subtitles');
    assert.equal(catchUpText(p), 'player.subtitleCatchUpWaitingShort', 'and says why');
    const waits = events('subtitle-translate-wait');
    assert.equal(waits.length, 1);
    assert.equal(waits[0].data.auto, true, 'told apart from a press of Wait');

    pending = '400';
    const playsBefore = log.play;
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(log.play > playsBefore, 'playback resumes once the run is ahead');
    assert.equal(behindBanner(p), null);
    const dones = events('subtitle-translate-wait-done');
    assert.equal(dones.length, 1);
    assert.equal(dones[0].data.auto, true);
});

test('the hold after a seek has no cap: it lasts until the run is ahead or the viewer says so', async (t) => {
    // Owner, 2026-09-18: the hold gave up after 20 s and played the film
    // without subtitles. Nothing on a timer ends it any more.
    let pending = '400';
    const { p, log, seek, events } = await mountSessionRun(t, () => catchUpResponse('12/400', pending));

    pending = '0';
    await seek();
    assert.equal(catchUpText(p), 'player.subtitleCatchUpWaitingShort');

    // Two poll windows of a run that stays behind: still held.
    const playsBefore = log.play;
    await wait(2 * POLL_INTERVAL_WINDOW_MS);
    assert.equal(log.play, playsBefore, 'nothing plays the film under a run that is still behind');
    assert.equal(catchUpText(p), 'player.subtitleCatchUpWaitingShort');
    assert.equal(events('subtitle-translate-wait-done').length, 0);

    // The viewer's way out is on the pill.
    click(catchUpBanner(p).querySelector('.wt-catchup-btn'));
    await settle();
    assert.ok(log.play > playsBefore, 'Keep watching plays it');
});

test('no hold when the seek happened on a paused film, or the service does not say where it is', async (t) => {
    await t.test('paused at the seek', async (tt) => {
        let pending = '400';
        const { p, log, seek, events } = await mountSessionRun(tt, () => catchUpResponse('12/400', pending));
        p.video.paused = true;
        pending = '0';
        const pausesBefore = log.pause;
        await seek();
        await wait(POLL_INTERVAL_WINDOW_MS);
        // The seek decides nothing for a film that was paused -- but the
        // seeker's native path plays the new run, and a film playing over
        // untranslated lines is held like any other (2026-09-18).
        assert.ok(log.pause > pausesBefore);
        assert.equal(events('subtitle-translate-wait').length, 1, 'by the ordinary rule, once');
    });
    await t.test('paused after the seek landed, before the answer came back', async (tt) => {
        let pending = '400';
        const { p, log, events } = await mountSessionRun(tt, () => catchUpResponse('12/400', pending));
        pending = '0';
        document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
        await settle();
        p.video.currentTime = 1;
        p.video.dispatchEvent(new dom.window.Event('playing'));
        // The viewer pauses in the same moment: the settle's request is
        // still in flight. Since 2026-09-18 the element is already paused
        // by then (the silent hold), so their pause arrives as a toggle --
        // which must mean "pause", not "play".
        click(p.video);
        await settle();
        assert.equal(p.video.paused, true, 'the toggle during the silent hold pauses');
        const pausesAfterViewer = log.pause;
        const playsAfterViewer = log.play;
        await wait(POLL_INTERVAL_WINDOW_MS);
        assert.equal(events('subtitle-translate-wait').length, 0, 'no hold for a film the viewer paused');
        assert.equal(log.pause, pausesAfterViewer);
        assert.equal(log.play, playsAfterViewer, 'and nothing plays it back on under them');
    });
    await t.test('older service without the frontier header', async (tt) => {
        const { p, log, seek, events } = await mountSessionRun(tt, () => liveProgressResponse('12/400'));
        const playsBefore = log.play;
        await seek();
        await settle();
        // The silent hold pauses for one answer and lets go on it: no wait,
        // no banner, and the film is playing again.
        assert.equal(events('subtitle-translate-wait').length, 0);
        assert.equal(behindBanner(p), null);
        assert.equal(p.video.paused, false, 'the first answer releases the film');
        assert.ok(log.play > playsBefore);
    });
});

// ---- the seek hold, after review ---------------------------------------

// A live answer that also says which run it describes.
const runResponse = (header, pendingFrom, sessionOffset) => ({
    ok: true,
    status: 200,
    headers: {
        get: (n) => {
            if (n === 'X-Subtitle-Progress') return header;
            if (n === 'X-Subtitle-Live') return '1';
            if (n === 'X-Subtitle-Pending-From') return pendingFrom;
            if (n === 'X-Subtitle-Session-Offset') return sessionOffset;
            return null;
        },
    },
    json: async () => ({}),
});

test('a pause while the answer after a seek is in flight cancels the seek\u2019s hold; a later play answers to the ordinary rule', async (t) => {
    // Found in review: the "hold after this seek" flag was only cleared by a
    // poll answer, a pause dropped that answer, and the next play — seconds
    // or minutes later — paused the film again.
    let pending = '400';
    const delayed = () => new Promise((resolve) => setTimeout(() => resolve(catchUpResponse('12/400', pending)), 200));
    const { p, log, events } = await mountSessionRun(t, delayed);
    await wait(300);
    pending = '0';
    document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await settle();
    p.video.currentTime = 1;
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await wait(20);
    // Through the UI, as a viewer does: the element is already paused by the
    // silent hold, and the toggle is what reads that as "pause".
    click(p.video);
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(p.video.paused, true, 'nothing played it back on under them');
    assert.equal(events('subtitle-translate-wait').length, 0, 'the seek\u2019s own hold is gone');
    p.video.play();
    await wait(POLL_INTERVAL_WINDOW_MS);
    // Since 2026-09-18 a film playing over untranslated lines is held
    // whatever brought it there; what must not happen is a hold the viewer
    // cannot get out of.
    assert.equal(events('subtitle-translate-wait').length, 1, 'held by the ordinary rule');
    click(catchUpBanner(p).querySelector('.wt-catchup-btn'));
    await settle();
    const pauses = log.pause;
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(log.pause, pauses, 'Keep watching is the end of it');
    assert.equal(events('subtitle-translate-wait').length, 1);
});

test('a run that dies during a seek\u2019s hold plays the film it paused', async (t) => {
    // The Wait button leaves the film paused when its run dies — the viewer
    // paused on purpose. A hold is a pause the viewer never asked for.
    let status = 200;
    let pending = '400';
    const { p, log, seek } = await mountSessionRun(t, () => (status === 200
        ? catchUpResponse('12/400', pending)
        : { ok: false, status, headers: { get: () => null } }));
    pending = '0';
    await seek();
    assert.equal(catchUpText(p), 'player.subtitleCatchUpWaitingShort');
    const plays = log.play;
    status = 500;
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(log.play > plays, 'the film plays again');
    assert.equal(behindBanner(p), null);
});

test('an answer about the run before the seek does not decide the hold; the one about the new run does', async (t) => {
    // Found in review, then measured on a real session: the transcoder lists
    // the new run ~200 ms after the seek POST, so the first answers after a
    // seek can describe the old run — with nothing pending, which read as
    // "caught up" and spent the decision.
    // Joined mid-film: the session's run starts at 300 s. ArrowLeft from
    // ~5:01 lands on the run starting at 270 s.
    let answer = () => runResponse('12/400', '800', '300.000');
    const { p, log, events } = await mountSessionRun(t, () => answer(), { sessionOffset: 300 });

    // The old run's answer, still: nothing pending, offset 300.
    answer = () => runResponse('12/400', null, '300.000');
    document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));
    await settle();
    p.video.currentTime = 1;
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await settle();
    assert.equal(events('subtitle-translate-wait').length, 0, 'not decided on the old run');
    const heads = p.calls.filter((c) => c.params && c.params.method === 'HEAD').map((c) => String(c.url));
    assert.ok(heads.at(-1).includes('sof=270'), `the polls name the run the player watches: ${heads.at(-1)}`);

    // The service reads the new run; the watch asks again within a second.
    answer = () => runResponse('12/400', '270.000', '270.000');
    await wait(1500);
    assert.equal(events('subtitle-translate-wait').length, 1, 'decided on the answer about the new run');
    assert.equal(events('subtitle-translate-wait')[0].data.auto, true);
    assert.ok(log.pause >= 1);
});

test('during a seek\u2019s hold: another seek plays the film and decides again; \u00d7 plays it too', async (t) => {
    let pending = '400';
    const { p, log, seek, events } = await mountSessionRun(t, () => catchUpResponse('12/400', pending));
    pending = '0';
    await seek();
    assert.equal(events('subtitle-translate-wait').length, 1);

    const plays = log.play;
    await seek();
    assert.ok(log.play > plays, 'a seek during a hold is not left paused behind it');
    assert.equal(events('subtitle-translate-wait').length, 2, 'and the new position gets its own hold');

    const beforeX = log.play;
    click(catchUpBanner(p).querySelector('.wt-catchup-close'));
    await settle();
    assert.ok(log.play > beforeX, '\u00d7 on a hold plays the film: the viewer never paused it');
    assert.equal(behindBanner(p), null);
});

test('a hold that ends while the tab is hidden does not start playback in the background', async (t) => {
    t.after(() => { delete document.hidden; });
    let pending = '400';
    const { p, log, seek, events } = await mountSessionRun(t, () => catchUpResponse('12/400', pending));
    pending = '0';
    await seek();
    assert.equal(events('subtitle-translate-wait').length, 1);
    setHidden(true);
    const plays = log.play;
    // The run gets ahead behind the viewer's back.
    pending = '400';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(events('subtitle-translate-wait-done').length, 1, 'the wait is over');
    assert.equal(log.play, plays, 'no playback in a hidden tab');

    // Found in re-review: the poll stayed awake behind the paused film, and
    // for a live source that keeps the transcode running for nobody.
    const heads = () => p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;
    const atEnd = heads();
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(heads(), atEnd, 'the poll sleeps while the tab is hidden');

    setHidden(false);
    await settle();
    assert.ok(log.play > plays, 'the film plays when the viewer is back');
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(heads() > atEnd, 'and the poll with it');
});

test('answers that show nothing pending yet do not spend the hold; the first one that does starts it', async (t) => {
    // Found in re-review: the first answers after a seek can predate the new
    // run's cues (the transcoder closes a subtitle segment only on the next
    // cue). One-shot, "nothing pending" read as caught up and the lines that
    // followed played bare. A silent stretch never shows anything pending.
    let pending = null;
    const { p, log, seek, events } = await mountSessionRun(t, () => catchUpResponse('12/400', pending));
    await seek();
    await wait(1200);
    assert.equal(events('subtitle-translate-wait').length, 0, 'nothing pending: no hold, but still watching');
    const heads = p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;
    assert.ok(heads >= 3, `the window asks about every second, not every 3 s: ${heads} HEADs`);

    pending = '0';
    await wait(1500);
    assert.equal(events('subtitle-translate-wait').length, 1, 'the cue that turned up starts the hold');
    assert.ok(log.pause >= 1);
});

test('past the watch window the seek decides nothing, and the ordinary rule takes over', async (t) => {
    const watch = catchUpTiming.seekWatchMs;
    catchUpTiming.seekWatchMs = 300;
    t.after(() => { catchUpTiming.seekWatchMs = watch; });
    let pending = null;
    const { p, seek, events } = await mountSessionRun(t, () => catchUpResponse('12/400', pending));
    await seek();
    await wait(1300);
    pending = '0';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(events('subtitle-translate-wait').length, 1, 'held once, by the rule every playing film answers to');
    assert.equal(catchUpText(p), 'player.subtitleCatchUpWaitingShort');
});

test('a tab hidden during the seek gets no hold while nobody is looking', async (t) => {
    t.after(() => { delete document.hidden; });
    let pending = '400';
    const { p, log, events } = await mountSessionRun(t, () => catchUpResponse('12/400', pending));
    pending = '0';
    document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await settle();
    setHidden(true);
    p.video.currentTime = 1;
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await wait(1500);
    assert.equal(events('subtitle-translate-wait').length, 0);
    assert.equal(log.pause, 0, 'nothing is decided in the background');
    // Back, and the film is playing over untranslated lines: the ordinary
    // rule (2026-09-18), not the seek, is what holds it now.
    setHidden(false);
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(events('subtitle-translate-wait').length, 1);
});

test('a run mismatch that does not go away stops silencing the banner', async (t) => {
    // Found in re-review: two sessions on one key, or a failed offset read
    // at mount, made every answer "about another run" for good: no banner,
    // a pressed Wait that never ends.
    const limit = catchUpTiming.runMismatchLimit;
    catchUpTiming.runMismatchLimit = 1;
    t.after(() => { catchUpTiming.runMismatchLimit = limit; });
    const { p } = await mountSessionRun(t, () => runResponse('12/400', '100.000', '900.000'), { sessionOffset: 90, startPaused: true });
    p.video.currentTime = 10;
    await wait(POLL_INTERVAL_WINDOW_MS * 2);
    assert.ok(catchUpBanner(p), 'the banner is back');
    // The player keeps naming its run (2026-09-19): dropping `sof` took the
    // re-read hint away from the service and made it file the viewer's
    // position under the run it was still on.
    const last = p.calls.filter((c) => c.params && c.params.method === 'HEAD').map((c) => String(c.url)).at(-1);
    assert.ok(last.includes('sof=90'), `still named: ${last}`);
});

test('a service that stays on another run: the film waits, and never hears "caught up"', async (t) => {
    // Owner, 2026-09-19: seek to 44:27, the service kept answering about the
    // run before the seek. Its document had no cue past 38:16, so "nothing
    // pending" came back, the hold ended and the pill flashed "caught up"
    // over a film with no subtitles. What the viewer has is what decides:
    // past every loaded cue there is nothing to show, so the film is held.
    const limit = catchUpTiming.runMismatchLimit;
    catchUpTiming.runMismatchLimit = 2;
    t.after(() => { catchUpTiming.runMismatchLimit = limit; });
    // The answer is always about the run at 0 s, with nothing pending.
    const { p, log, events } = await mountSessionRun(t, () => runResponse('375/420', null, '0.000'), { sessionOffset: 2667, startPaused: true });
    const el = p.video.querySelector('track#tr-pt');
    el.track.cues.length = 0;
    el.track.addCue({ startTime: -1, endTime: -1, __absStart: 2290, __absEnd: 2296 }); // 38:16, the old run's last line
    p.video.currentTime = 5; // 44:32 of the film
    await wait(POLL_INTERVAL_WINDOW_MS * 3);

    assert.equal(doneBanner(p), null, 'never "caught up"');
    assert.equal(p.video.paused, true, 'no subtitles where the viewer is: the film waits');
    assert.equal(catchUpText(p), 'player.subtitleCatchUpWaitingShort');
    const waits = events('subtitle-translate-wait');
    assert.equal(waits.length, 1, 'one hold, not one per tick');
    assert.equal(waits[0].data.auto, true);
    const last = p.calls.filter((c) => c.params && c.params.method === 'HEAD').map((c) => String(c.url)).at(-1);
    assert.ok(last.includes('sof=2667'), `and the run is still named, so the service can come round: ${last}`);

    // The way out is the viewer's, and it holds: no second pause.
    click(catchUpBanner(p).querySelector('.wt-catchup-btn'));
    await settle();
    const pauses = log.pause;
    await wait(POLL_INTERVAL_WINDOW_MS * 2);
    assert.equal(p.video.paused, false, 'Keep watching plays the film');
    assert.equal(log.pause, pauses, 'and it is not held again');
    assert.equal(catchUpText(p), 'player.subtitleCatchUpShort', 'the pill goes on offering');
    assert.equal(doneBanner(p), null);
});

test('a service that comes round ends that wait the ordinary way', async (t) => {
    const limit = catchUpTiming.runMismatchLimit;
    catchUpTiming.runMismatchLimit = 2;
    t.after(() => { catchUpTiming.runMismatchLimit = limit; });
    let answer = () => runResponse('375/420', null, '0.000');
    const { p, log, events } = await mountSessionRun(t, () => answer(), { sessionOffset: 2667, startPaused: true });
    p.video.currentTime = 5;
    await wait(POLL_INTERVAL_WINDOW_MS * 3);
    assert.equal(p.video.paused, true, 'fixture: held');

    // The service reads the new run and is well ahead of the viewer.
    answer = () => runResponse('40/80', '2900.000', '2667.000');
    const plays = log.play;
    await wait(POLL_INTERVAL_WINDOW_MS * 2);
    assert.ok(log.play > plays, 'the film goes on');
    assert.equal(behindBanner(p), null);
    assert.equal(events('subtitle-translate-wait-done').length, 1);
});

test('with cues still ahead, a permanent run mismatch is taken at its word as before', async (t) => {
    const limit = catchUpTiming.runMismatchLimit;
    catchUpTiming.runMismatchLimit = 2;
    t.after(() => { catchUpTiming.runMismatchLimit = limit; });
    const { p } = await mountSessionRun(t, () => runResponse('375/420', null, '0.000'), { sessionOffset: 600, startPaused: true });
    const el = p.video.querySelector('track#tr-pt');
    el.track.cues.length = 0;
    el.track.addCue({ startTime: 100, endTime: 103, __absStart: 700, __absEnd: 703 });
    p.video.currentTime = 5; // 10:05, a minute and a half short of the loaded edge
    await wait(POLL_INTERVAL_WINDOW_MS * 3);
    assert.equal(catchUpBanner(p), null, 'nothing pending, cues ahead: nothing to say');
});

// A timeout of its own: without the fix the seek waits for a `playing` that
// never comes, and the suite would hang instead of failing.
test('a refused seek POST moves nothing: no offset change, no reload, no frozen frame', { timeout: 5000 }, async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => { it.installHls({ subtitleTrack: -1, subtitleDisplay: false }); });
    p.setResponse((url, params) => (params && params.method === 'POST'
        ? { ok: false, status: 503, json: async () => ({}) }
        : { ok: true, status: 200, json: async () => ({}) }));
    const hls = window.hlsPlayer;
    let loads = 0;
    const origLoad = hls.loadSource;
    hls.loadSource = (...a) => { loads++; return origLoad ? origLoad.apply(hls, a) : undefined; };
    const offsets = [];
    const seeking = [];
    const seeker = createSessionSeeker({
        hls, videoEl: p.video, sessionSeekUrl: '/session/seek', sourceUrl: 'https://x.test/index.m3u8',
        trackContainer: p.container, onSeekOffsetChange: (o) => offsets.push(o), onSeekingChange: (v) => seeking.push(v),
    });
    const orig = console.error;
    console.error = () => {};
    t.after(() => { console.error = orig; });
    await seeker.seek(120);
    assert.deepEqual(offsets, [], 'the offset stays with the run the transcoder is still on');
    assert.equal(loads, 0, 'no reload');
    assert.deepEqual(seeking, [true, false], 'and the seek is over');
});

// ---- the same logic for a file-source translation -----------------------
//
// A translation of a file source (an OpenSubtitles track) now answers the
// same frontier header, computed against the playhead the poll itself
// carries — so the banner, Wait and the seek hold work identically, gated
// on the header's presence rather than on X-Subtitle-Live.

const fileResponse = (header, pendingFrom) => ({
    ok: true,
    status: 200,
    headers: {
        get: (n) => {
            if (n === 'X-Subtitle-Progress') return header;
            if (n === 'X-Subtitle-Pending-From') return pendingFrom;
            return null;
        },
    },
    json: async () => ({}),
});

test('a file translation that is behind the playhead offers to wait too', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    let pending = '100';
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? fileResponse('12/400', pending)
        : { ok: true, status: 200, json: async () => ({}) }));
    p.video.paused = false;
    p.video.currentTime = 100;
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();
    assert.ok(catchUpBanner(p), 'the banner needs only the frontier, not a live source');
    pending = '400';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(behindBanner(p), null);
    const heads = p.calls.filter((c) => c.params && c.params.method === 'HEAD').map((c) => String(c.url));
    assert.ok(heads.every((u) => u.includes('pos=')), `every poll says where the viewer is: ${heads[0]}`);
    assert.ok(!heads.at(-1).includes('sof='), 'and a file source never names a run');
});

test('a direct seek (no transcoder session) opens the hold window like a session seek', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    p.video.setAttribute('data-duration', '3600');
    const log = playback(p.video);
    let pending = '3600';
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? fileResponse('12/400', pending)
        : { ok: true, status: 200, json: async () => ({}) }));
    p.video.paused = false;
    p.video.currentTime = 100;
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();
    assert.equal(behindBanner(p), null, 'far ahead: nothing to say');

    // The viewer seeks 15 s forward into film the job has not reached.
    pending = '0';
    document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await wait(1200);
    const waits = p.events.filter((e) => e.name === 'subtitle-translate-wait');
    assert.equal(waits.length, 1, 'the hold starts without any session settle');
    assert.equal(waits[0].data.auto, true);
    assert.ok(log.pause >= 1);

    pending = '3600';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(log.play >= 1, 'and ends when the job is ahead again');
    assert.equal(behindBanner(p), null);
});

test('holding an arrow key rations the direct-seek kicks', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    p.video.setAttribute('data-duration', '3600');
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? fileResponse('12/400', '3600')
        : { ok: true, status: 200, json: async () => ({}) }));
    p.video.paused = false;
    p.video.currentTime = 100;
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();
    const heads = () => p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;
    const before = heads();
    // Spaced like a real key repeat: each kick's immediate tick has time to
    // fire before the next keydown, so an unrationed kick is one HEAD per
    // repeat rather than thirty collapsed timers.
    for (let i = 0; i < 20; i++) {
        document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
        await wait(15);
    }
    await settle();
    assert.ok(heads() - before <= 3, `20 spaced repeats must not be 20 HEADs: ${heads() - before}`);
});

test('the banner on a file source claims no cue count — the whole-film remainder would be a lie', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? fileResponse('60/1200', '100')
        : { ok: true, status: 200, json: async () => ({}) }));
    await pickPausedThenPlay(p, 100);
    // A file run does not sleep with the film, so pressing play wakes
    // nothing: the banner is decided by the next ordinary tick.
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(catchUpText(p), 'player.subtitleCatchUpWaitingShort', 'the copy without the count');
});

test('a session seek in flight sends no position: the halves disagree mid-seek', async (t) => {
    let pending = '4000';
    const { p, seek } = await mountSessionRun(t, () => catchUpResponse('12/400', pending), { sessionOffset: 1500 });
    // Playing 300 s into a run that starts at 1500 s; the poll says 1800.
    p.video.currentTime = 300;
    await wait(POLL_INTERVAL_WINDOW_MS);
    const urls = () => p.calls.filter((c) => c.params && c.params.method === 'HEAD').map((c) => String(c.url));
    assert.ok(urls().at(-1).includes('pos=1800'), `before the seek: ${urls().at(-1)}`);
    // The seek to ~5:15 kicks a poll while currentTime still belongs to the
    // old run: that poll must carry no pos at all rather than 300+315.
    document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await settle();
    const during = urls().at(-1);
    assert.ok(!during.includes('pos='), `mid-seek, no position: ${during}`);
    p.video.currentTime = 1;
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await settle();
    assert.ok(urls().at(-1).includes('pos='), `settled: the position is back: ${urls().at(-1)}`);
});

test('the seek uses the offset the transcoder answers, not its own quantized guess', async (t) => {
    // A copy-mode run starts at the keyframe before the quantized point;
    // the POST now says where. Cues shifted by the local floor(t/30)*30 ran
    // ahead of the sound by the difference (the "small desync after a seek"
    // report, 2026-09-17).
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => { it.installHls({ subtitleTrack: -1, subtitleDisplay: false }); });
    p.setResponse((url, params) => (params && params.method === 'POST'
        ? { ok: true, status: 200, json: async () => ({ ok: true, offset: 115.633 }) }
        : { ok: true, status: 200, json: async () => ({}) }));
    const offsets = [];
    const seeker = createSessionSeeker({
        hls: window.hlsPlayer, videoEl: p.video, sessionSeekUrl: '/session/seek',
        sourceUrl: 'https://x.test/index.m3u8', trackContainer: p.container,
        onSeekOffsetChange: (o) => offsets.push(o),
    });
    const seeking = seeker.seek(120);
    await flush();
    window.hlsPlayer.emit(Hls.Events.SUBTITLE_TRACKS_UPDATED, {});
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await seeking;
    assert.deepEqual(offsets, [115.633], 'the answered real start, not 120 quantized to 120');
});

test('a transcoder that does not answer an offset leaves the quantized guess in place', async (t) => {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => { it.installHls({ subtitleTrack: -1, subtitleDisplay: false }); });
    p.setResponse((url, params) => (params && params.method === 'POST'
        ? { ok: true, status: 200, json: async () => ({ ok: true }) }
        : { ok: true, status: 200, json: async () => ({}) }));
    const offsets = [];
    const seeker = createSessionSeeker({
        hls: window.hlsPlayer, videoEl: p.video, sessionSeekUrl: '/session/seek',
        sourceUrl: 'https://x.test/index.m3u8', trackContainer: p.container,
        onSeekOffsetChange: (o) => offsets.push(o),
    });
    const seeking = seeker.seek(125);
    await flush();
    window.hlsPlayer.emit(Hls.Events.SUBTITLE_TRACKS_UPDATED, {});
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await seeking;
    assert.deepEqual(offsets, [120], 'floor(125/30)*30, as before the transcoder said');
});

// ---- a saved track survives the initial loadSource ----------------------
//
// A saved side-loaded selection is restored before the HLS instance exists,
// so the initial loadSource wipes its parsed cues exactly as a seek's does —
// loaded, showing, and empty for the whole session (reproduced on stage
// 2026-09-17, no seek anywhere). createHls marks the tracks before its
// loadSource; this test pins the half that follows: initDefaultTracks'
// apply refetches the marked, wiped track.
test('the restored default track wiped by the initial loadSource is fetched again', async (t) => {
    const { markUnsnapshottedTracksStale } = await import('./subtitle-track-reload.js');
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => {
        it.chip('none').removeAttribute('data-default');
        it.chip('os-os-ru').setAttribute('data-default', 'true');
    }, { tracks: [['os-os-ru', true]] });
    const el = p.video.querySelector('track#os-os-ru');
    Object.defineProperty(el, 'readyState', { configurable: true, value: 2 });

    // What createHls does around its loadSource: mark, wipe.
    markUnsnapshottedTracksStale(Array.from(p.video.querySelectorAll('track')), []);
    // (the track list is already empty under jsdom, which is the wiped state)

    // MANIFEST_PARSED → initDefaultTracks applies the saved selection.
    initDefaultTracks(window.hlsPlayer || makeHls({ video: p.video }), p.video);
    assert.match(el.getAttribute('src'), /wt-rf=\d+$/, 'the wiped restored track is fetched again');
    assert.equal(p.mode('os-os-ru'), 'showing');
});

test('a seek answer whose body never completes does not lock seeking', { timeout: 8000 }, async (t) => {
    // Found in review: isSeeking is cleared only in catch or on `playing`,
    // and an unbounded res.json() sat before both — one stalled body and
    // every later seek returned immediately for the rest of the session.
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => { it.installHls({ subtitleTrack: -1, subtitleDisplay: false }); });
    p.setResponse((url, params) => (params && params.method === 'POST'
        ? { ok: true, status: 200, json: () => new Promise(() => {}) }
        : { ok: true, status: 200, json: async () => ({}) }));
    const offsets = [];
    const seeker = createSessionSeeker({
        hls: window.hlsPlayer, videoEl: p.video, sessionSeekUrl: '/session/seek',
        sourceUrl: 'https://x.test/index.m3u8', trackContainer: p.container,
        onSeekOffsetChange: (o) => offsets.push(o),
    });
    const seeking = seeker.seek(125);
    await wait(3500);
    window.hlsPlayer.emit(Hls.Events.SUBTITLE_TRACKS_UPDATED, {});
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await seeking;
    assert.deepEqual(offsets, [120], 'the stalled body is abandoned and the quantized fallback applies');
    assert.equal(seeker.isSeeking(), false, 'and the seek completes');
});

// ---- the on-screen translation offer -----------------------------------
//
// The pill is the picker's offer brought to the picture (subtitle-offer.js).
// These mount the real component: what is under test is the chain from
// "playback started" to a pill, and from a click on it to the same run a
// click on the chip starts.

const offerPill = (p) => p.container.querySelector('.wt-offer');
const offerCard = (p) => p.container.querySelector('.wt-offer-card');
const startPlayback = async (p) => {
    p.video.paused = false;
    p.video.dispatchEvent(new dom.window.Event('play'));
    await settle();
};
// The free-viewer render of the AI chip, as helper.go marks it.
const lockChip = (p, { upsell = true } = {}) => {
    const ai = p.chip('tr-pt');
    ai.setAttribute('data-locked', 'true');
    ai.removeAttribute('data-src');
    ai.removeAttribute('data-offered');
    if (upsell) ai.setAttribute('data-upsell', 'true');
};
const clearOfferMemory = () => { try { window.localStorage.clear(); } catch (e) { /* none */ } };

test('the offer appears when playback starts, and taking it is pressing the chip', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? progressResponse('0/0')
        : { ok: true, status: 200, json: async () => ({}) }));
    assert.equal(offerPill(p), null, 'nothing is offered on a page that has not played');

    await startPlayback(p);
    const pill = offerPill(p);
    assert.ok(pill, 'the fixture offers a translation, so the pill is up');
    assert.equal(pill.dataset.kind, 'start');
    assert.ok(p.events.some((e) => e.name === 'subtitle-offer-shown' && e.data.kind === 'start'));

    click(pill.querySelector('.wt-offer-main'));
    await settle();

    const ai = p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]');
    assert.ok(active(ai), 'the chip took the mark');
    assert.equal(p.puts().length, 1, 'and the choice was saved, as a chip click saves it');
    assert.equal(p.events.filter((e) => e.name === 'subtitle-translate-start').length, 1, 'one run started');
    assert.equal(offerPill(p), null, 'an offer taken is an offer spent');
});

test('picking a track in the picker withdraws the offer', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    await startPlayback(p);
    assert.ok(offerPill(p));
    const other = Array.from(p.container.querySelectorAll('#subtitle-tracks .subtitle[data-id]'))
        .find((el) => el.getAttribute('data-id') !== 'none' && el.getAttribute('data-provider') !== 'Translated');
    assert.ok(other, 'the fixture must carry a human track');
    click(other);
    await settle();
    assert.equal(offerPill(p), null);
});

test('the × on a start offer is for this page only', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    await startPlayback(p);
    click(offerPill(p).querySelector('.wt-catchup-close'));
    await settle();
    assert.equal(offerPill(p), null);
    assert.equal(window.localStorage.getItem('wt-subtitle-offer'), null, 'nothing is remembered about an action the viewer can take');
});

test('a free viewer gets the upsell: a card, a CTA, and a way to never see it again', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer((page) => lockChip(page));
    await startPlayback(p);
    const pill = offerPill(p);
    assert.ok(pill);
    assert.equal(pill.dataset.kind, 'upsell');
    assert.equal(offerCard(p), null, 'the card waits for a click');

    click(pill.querySelector('.wt-offer-main'));
    await settle();
    const card = offerCard(p);
    assert.ok(card);
    const cta = card.querySelector('a.wt-offer-card-cta');
    assert.ok(cta.getAttribute('href').endsWith('/donate'), 'the picker’s own link');
    assert.equal(cta.getAttribute('target'), '_blank', 'paying must not cost the viewer the film');
    assert.equal(p.puts().length, 0, 'nothing was selected');
    assert.equal(p.events.filter((e) => e.name === 'subtitle-translate-start').length, 0, 'and no run starts');

    const never = Array.from(card.querySelectorAll('.wt-offer-card-link')).find((b) => b.textContent === 'player.subtitleOfferNever');
    click(never);
    await settle();
    assert.equal(offerPill(p), null);
    assert.equal(offerCard(p), null);
    assert.deepEqual(JSON.parse(window.localStorage.getItem('wt-subtitle-offer')), { never: true });

    // The next film: same viewer, same state of the list.
    destroyPlayer();
    const again = await mountPlayer((page) => lockChip(page));
    await startPlayback(again);
    assert.equal(offerPill(again), null, 'asked not to be offered, not offered');
});

test('the × on the upsell is thirty days, not forever', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer((page) => lockChip(page));
    await startPlayback(p);
    const before = Date.now();
    click(offerPill(p).querySelector('.wt-catchup-close'));
    await settle();
    const saved = JSON.parse(window.localStorage.getItem('wt-subtitle-offer'));
    assert.equal(saved.never, undefined);
    const days = (saved.until - before) / 86400000;
    assert.ok(days > 29.9 && days < 30.1, `until is ${days} days out`);
});

test('a locked translation the ladder did not want is not pitched on screen', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer((page) => lockChip(page, { upsell: false }));
    await startPlayback(p);
    assert.equal(offerPill(p), null);
});

// ---- the resume prompt holds the film ----------------------------------

const savedPosition = (p) => p.setResponse((url) => (String(url).startsWith('/watch/position')
    ? { ok: true, status: 200, headers: new dom.window.Headers(), json: async () => ({ position: 600, duration: 3000 }) }
    : { ok: true, status: 200, headers: new dom.window.Headers(), json: async () => ({}) }));
const resumePrompt = (p) => p.container.querySelector('.wt-resume-prompt');

test('a film with a saved position does not start by itself, and is not pitched under the prompt', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer((page) => savedPosition(page));
    const log = playback(p.video);
    assert.ok(resumePrompt(p), 'the fixture must put the prompt up');

    // <video autoplay> firing after the prompt is already up.
    p.video.play();
    await settle();
    assert.equal(p.video.paused, true, 'autoplay is put back to sleep');
    assert.ok(log.pause >= 1);
    assert.equal(offerPill(p), null, 'no offer behind the overlay');

    // Start over: the answer is what starts the film, and the offer follows.
    const playsBefore = log.play;
    click(resumePrompt(p).querySelector('.wt-resume-btn--ghost'));
    await settle();
    assert.equal(resumePrompt(p), null);
    assert.ok(log.play > playsBefore, 'the answer starts playback');
    assert.equal(p.video.paused, false, 'and nothing pauses it again');
    assert.ok(offerPill(p), 'now the offer is decided');
});

// ---- the dialog is re-rendered, the player is not -------------------------
//
// Owner, 2026-09-19: changing the preferred language re-rendered the whole
// player, and a second of nothing is long in the middle of a film. The new
// dialog is rendered off the page (background-render.js) and put in place of
// the old one; these pin what "in place" has to mean.

// freshDialog is the dialog a render for another language would bring: the
// fixture again, with the language changed and the AI chip renamed.
function freshDialog(lang) {
    const doc = new dom.window.DOMParser().parseFromString(DIALOG, 'text/html');
    const dialog = doc.querySelector('#subtitles');
    dialog.setAttribute('data-preferred-lang', lang);
    const ai = dialog.querySelector('.subtitle[data-provider="Translated"]');
    ai.setAttribute('data-id', `tr-${lang}`);
    ai.setAttribute('data-srclang', lang);
    ai.setAttribute('data-src', (ai.getAttribute('data-src') || '').replace(/~tr:[a-z]+/, `~tr:${lang}`));
    const select = dialog.querySelector('#preferred-lang');
    if (select) select.setAttribute('data-current', lang);
    return dialog;
}

test('a dialog swap leaves the player alone and the new dialog works', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    const video = p.video;
    const liveDialog = p.modal;
    const oldBox = liveDialog.querySelector('.modal-box');
    p.video.paused = false;

    assert.equal(swapSubtitlesDialog(p.container, freshDialog('kk'), {}), true);

    assert.equal(p.container.querySelector('video.player'), video, 'the same <video>');
    assert.equal(p.container.querySelector('#subtitles'), liveDialog, 'the same dialog element: it is open, and the listeners hang on it');
    assert.notEqual(liveDialog.querySelector('.modal-box'), oldBox, 'with a new box in it');
    assert.equal(liveDialog.getAttribute('data-preferred-lang'), 'kk');
    assert.ok(findSubtitleItem(liveDialog, 'tr-kk'), 'the new language\u2019s AI chip');
    assert.equal(findSubtitleItem(liveDialog, 'tr-pt'), null, 'and not the old one');

    // The chips of the new box answer to the listeners of the old dialog.
    const chip = Array.from(liveDialog.querySelectorAll('.subtitle'))
        .find((el) => el.getAttribute('data-provider') === 'OpenSubtitles');
    click(chip);
    await settle();
    assert.equal(chip.getAttribute('data-default'), 'true', 'a chip in the new box can be picked');
    // ...and so does the switch, which is a new element too.
    const toggle = liveDialog.querySelector('#subtitles-toggle');
    toggle.checked = false;
    toggle.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
    await settle();
    assert.equal(liveDialog.getAttribute('data-subtitles-off'), 'true', 'the new switch switches');
});

test('a dialog swap takes the <track> of the language that was left', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? progressResponse('12/400')
        : { ok: true, status: 200, json: async () => ({}) }));
    p.video.paused = false;
    click(p.chip('tr-pt'));
    await settle();
    const heads = () => p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;
    assert.ok(heads() >= 1, 'fixture: a Portuguese run is polling');
    assert.ok(p.video.querySelector('track#tr-pt'), 'fixture: with its <track>');

    assert.equal(swapSubtitlesDialog(p.container, freshDialog('kk'), {}), true);
    await settle();
    assert.equal(p.video.querySelector('track#tr-pt'), null, 'the old language\u2019s <track> goes with its chip');
});

test('a player that comes back from a settings change continues without asking', async (t) => {
    // The preferred-language select re-renders the player (preferred-lang.js):
    // the viewer changed a setting, they did not leave, so the "continue
    // from" prompt is answered for them. Once, for this file, for a minute.
    t.after(() => { destroyPlayer(); window.sessionStorage.clear(); });
    clearOfferMemory();
    window.sessionStorage.setItem('wt-auto-resume', JSON.stringify({ resourceID: 'res', path: 'movie.mkv', until: Date.now() + 60000 }));
    const p = mount();
    savedPosition(p);
    const log = playback(p.video);
    await initPlayer(p.container);
    await settle();
    await settle();
    assert.equal(resumePrompt(p), null, 'nothing to answer');
    assert.equal(p.video.currentTime, 600, 'at the saved position');
    assert.ok(log.play >= 1 && !p.video.paused, 'and playing');
    assert.equal(window.sessionStorage.getItem('wt-auto-resume'), null, 'the note is spent');
});

test('without that note the prompt is asked as before', async (t) => {
    t.after(() => { destroyPlayer(); window.sessionStorage.clear(); });
    clearOfferMemory();
    window.sessionStorage.setItem('wt-auto-resume', JSON.stringify({ resourceID: 'another', path: 'movie.mkv', until: Date.now() + 60000 }));
    const p = await mountPlayer((page) => savedPosition(page));
    assert.ok(resumePrompt(p), 'a note for another file answers nothing here');
});

test('an audio file with a saved position is not held: it has no prompt to answer', async (t) => {
    // Found in review (2026-09-18): the hold had no isVideo guard while the
    // prompt renders for video only, so the track was re-paused on every
    // play with nothing on screen that could end it.
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer((page) => savedPosition(page), { tag: 'audio' });
    const log = playback(p.video);
    assert.equal(resumePrompt(p), null);
    p.video.play();
    await settle();
    assert.equal(log.pause, 0, 'nobody pauses it');
    assert.equal(p.video.paused, false);
});

test('a resume answered while a seek is in flight is carried out when that seek lets go', async (t) => {
    // Found in review (2026-09-18): handleSeek returns at once during a
    // session seek, and handleResume had already closed the prompt -- the
    // film stayed paused at the old position with nothing left to press.
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = mount();
    p.video.dataset.sessionId = 's1';
    p.video.dataset.sessionSeekUrl = '/session/seek';
    p.video.setAttribute('data-duration', '3000');
    let releasePosition;
    p.setResponse((url, params) => {
        if (String(url).startsWith('/watch/position') && !(params && params.method === 'POST')) {
            return new Promise((resolve) => {
                releasePosition = () => resolve({ ok: true, status: 200, headers: new dom.window.Headers(), json: async () => ({ position: 600, duration: 3000 }) });
            });
        }
        if (params && params.method === 'POST') return { ok: true, status: 200, headers: new dom.window.Headers(), json: async () => ({ ok: true }) };
        return { ok: true, status: 200, headers: new dom.window.Headers(), json: async () => ({ offset: 0 }) };
    });
    await initPlayer(p.container);
    await settle();
    const log = playback(p.video);
    const seekPosts = () => p.calls.filter((c) => c.params && c.params.method === 'POST' && String(c.url).startsWith('/session/seek')).map((c) => String(c.url));

    // An arrow key just before the saved position arrives.
    p.video.paused = false;
    document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await settle();
    assert.equal(seekPosts().length, 1, 'fixture: a seek is in flight');
    releasePosition();
    await settle();
    assert.ok(resumePrompt(p), 'fixture: the prompt came up over it');

    click(resumePrompt(p).querySelector('.wt-resume-btn:not(.wt-resume-btn--ghost)'));
    await settle();
    assert.equal(resumePrompt(p), null);
    assert.equal(seekPosts().length, 1, 'nothing can be sought yet');

    // The first seek lands; the answer is carried out.
    const playsBefore = log.play;
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await settle();
    assert.equal(seekPosts().length, 2, 'the resume seek follows');
    assert.ok(/[?&]t=600\b/.test(seekPosts()[1]), seekPosts()[1]);
    assert.ok(log.play > playsBefore, 'which starts the new run itself');
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await settle();
    assert.equal(p.video.paused, false, 'and the film is playing');
});

// A saved live translation, restored at mount behind the resume prompt:
// asleep until the viewer answers (nobody is watching), and awake after.
// Owner, 2026-09-18: "Kazakh is selected, no subtitles, until I press the
// chip again" -- the restored run had 7 cues and never asked for more.
async function mountSavedLiveRun(t, { session }) {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = mount({ tracks: [['tr-pt', false]] });
    if (session) {
        p.video.dataset.sessionId = 's1';
        p.video.dataset.sessionSeekUrl = '/session/seek';
    }
    p.video.setAttribute('data-duration', '3000');
    const ai = p.chip('tr-pt');
    ai.setAttribute('data-saved', 'true');
    ai.setAttribute('data-default', 'true');
    ai.removeAttribute('data-offered');
    p.chip('none').removeAttribute('data-default');
    p.modal.setAttribute('data-subtitles-off', 'false');
    let done = 7;
    p.setResponse((url, params) => {
        if (params && params.method === 'HEAD') return catchUpResponse(`${done}/${done}`, null);
        if (String(url).startsWith('/watch/position') && !(params && params.method === 'POST')) {
            return { ok: true, status: 200, headers: new dom.window.Headers(), json: async () => ({ position: 600, duration: 3000 }) };
        }
        if (params && params.method === 'POST') return { ok: true, status: 200, headers: new dom.window.Headers(), json: async () => ({ ok: true }) };
        return { ok: true, status: 200, headers: new dom.window.Headers(), json: async () => ({ offset: 0 }) };
    });
    const log = playback(p.video);
    await initPlayer(p.container);
    await settle();
    // <video autoplay>, which the prompt's hold puts back to sleep.
    p.video.play();
    await settle();
    const heads = () => p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;
    return { p, log, heads, more: (n) => { done = n; } };
}

for (const session of [false, true]) {
    for (const answer of ['resume', 'start over']) {
        test(`a saved live translation wakes when the prompt is answered (${answer}, ${session ? 'session' : 'direct'})`, async (t) => {
            const { p, heads, more } = await mountSavedLiveRun(t, { session });
            assert.ok(resumePrompt(p), 'fixture: the prompt is up');
            assert.equal(p.video.paused, true, 'fixture: and the film is held');
            await wait(POLL_INTERVAL_WINDOW_MS);
            const asleep = heads();
            assert.ok(asleep >= 1, 'fixture: the restored run asked at least once');
            await wait(POLL_INTERVAL_WINDOW_MS);
            assert.equal(heads(), asleep, 'fixture: asleep while nobody watches');

            more(40);
            click(resumePrompt(p).querySelector(answer === 'resume' ? '.wt-resume-btn--primary' : '.wt-resume-btn--ghost'));
            await settle();
            if (session && answer === 'resume') {
                p.video.currentTime = 1;
                p.video.dispatchEvent(new dom.window.Event('playing'));
                await settle();
            }
            await wait(POLL_INTERVAL_WINDOW_MS);
            // The HEADs are the claim. The swap to the new revision is the
            // reload throttle's and needsReload's business (tested above).
            assert.ok(heads() > asleep, `the run asks again once the film plays: ${heads()} vs ${asleep}`);
            assert.deepEqual(p.events.filter((e) => e.name === 'subtitle-translate-error'), []);
        });
    }
}

test('a film with no saved position still starts by itself', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    const log = playback(p.video);
    p.video.play();
    await settle();
    assert.equal(resumePrompt(p), null);
    assert.equal(log.pause, 0, 'nobody holds a film that has nothing to resume');
    assert.equal(p.video.paused, false);
});

// ---- starting a translation over a playing film ------------------------
//
// Owner, 2026-09-18: pressing "AI translate" did nothing visible -- no
// banner, no subtitles -- because a run that has counted nothing sends no
// frontier, and no frontier reads as "not behind". A start is the same
// event as a seek landing on untranslated film: the same hold, the same cap.

test('starting a translation over a playing film holds it until the run is ahead', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    const log = playback(p.video);
    // Registered nothing yet: `0/0`, no frontier header at all.
    let answer = () => progressResponse('0/0');
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? answer()
        : { ok: true, status: 200, json: async () => ({}) }));

    p.video.paused = false;
    p.video.currentTime = 100;
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();

    assert.ok(log.pause >= 1, 'the film is held');
    assert.equal(p.video.paused, true);
    assert.ok(catchUpBanner(p), 'and the banner says why');
    assert.equal(catchUpText(p), 'player.subtitleCatchUpWaitingShort', 'with no cue count: nothing has been counted');
    assert.ok(p.events.some((e) => e.name === 'subtitle-translate-wait' && e.data.auto === true));

    // The run counts its cues and is comfortably ahead of the playhead.
    answer = () => catchUpResponse('40/400', '400');
    const playsBefore = log.play;
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(log.play > playsBefore, 'the film goes back on by itself');
    assert.equal(behindBanner(p), null);
});

test('starting a translation over a paused film holds nothing', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    const log = playback(p.video);
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? progressResponse('0/0')
        : { ok: true, status: 200, json: async () => ({}) }));
    // jsdom's video starts paused; the viewer picks the track before playing.
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();
    assert.equal(log.pause, 0);
    assert.equal(behindBanner(p), null, 'a paused film is not running into anything');
    assert.equal(p.events.filter((e) => e.name === 'subtitle-translate-wait').length, 0);
});

test('the film a hold releases has its subtitles: the reload does not wait out the throttle', async (t) => {
    // Owner, 2026-09-18: after the start hold the film went on with no
    // subtitles, and they appeared only after pause -> play (which kicks the
    // poll, and a kick bypasses the throttle). The first counted answer had
    // spent the 15 s reload window on a near-empty revision.
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer(null, { tracks: [['tr-pt', false]] });
    const log = playback(p.video);
    let answer = () => catchUpResponse('0/400', '100');
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? answer()
        : { ok: true, status: 200, json: async () => ({}) }));
    const trackSrc = () => p.video.querySelector('track#tr-pt').getAttribute('src') || '';

    p.video.paused = false;
    p.video.currentTime = 100;
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();
    assert.equal(p.video.paused, true, 'held: counted, nothing done, frontier at the playhead');
    assert.ok(!/rev=0\b/.test(trackSrc()), `a revision with nothing translated is not worth a swap: ${trackSrc()}`);

    // A few cues land, still behind: an ordinary reload, which starts the
    // 15 s throttle window.
    answer = () => catchUpResponse('5/400', '101');
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(/rev=5\b/.test(trackSrc()), `the first translated cues are loaded: ${trackSrc()}`);
    assert.equal(p.video.paused, true, 'still held');

    // Comfortably ahead, well inside the throttle window.
    answer = () => catchUpResponse('40/400', '400');
    const playsBefore = log.play;
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(log.play > playsBefore, 'the hold lets the film go');
    assert.ok(/rev=40\b/.test(trackSrc()), `and the track it goes on with is the current one: ${trackSrc()}`);
});

// ---- the silent hold ----------------------------------------------------

test('a seek onto untranslated film never plays before it is held', async (t) => {
    // Owner, 2026-09-18: the film started after a seek and was paused a
    // moment later, once the answer arrived. The pause now comes with the
    // seek settling; the answer only decides what it turns into.
    let pending = '400';
    const delayed = () => new Promise((resolve) => setTimeout(() => resolve(catchUpResponse('12/400', pending)), 200));
    const { p, log, events } = await mountSessionRun(t, delayed);
    await wait(300);
    pending = '0';
    document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await settle();
    p.video.currentTime = 1;
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await wait(20);

    // The answer is still 180 ms out.
    assert.equal(p.video.paused, true, 'held the moment the seek settled');
    assert.equal(behindBanner(p), null, 'with nothing said yet');
    assert.equal(events('subtitle-translate-wait').length, 0);
    const playsWhileSilent = log.play;

    await wait(400);
    assert.equal(events('subtitle-translate-wait').length, 1, 'the answer turns it into the ordinary hold');
    assert.ok(catchUpBanner(p), 'which has a banner');
    assert.equal(log.play, playsWhileSilent, 'and the film never played in between');
    assert.equal(p.video.paused, true);
});

test('a silent hold nobody answers lets the film go', async (t) => {
    const cap = catchUpTiming.seekPreHoldMaxMs;
    catchUpTiming.seekPreHoldMaxMs = 300;
    t.after(() => { catchUpTiming.seekPreHoldMaxMs = cap; });
    let hang = false;
    const { p, log, events } = await mountSessionRun(t, () => (hang
        ? new Promise(() => {})
        : catchUpResponse('12/400', '400')));
    hang = true;
    const playsBefore = log.play;
    document.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await settle();
    p.video.currentTime = 1;
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await wait(20);
    assert.equal(p.video.paused, true);
    await wait(500);
    assert.equal(p.video.paused, false, 'the cap is what bounds a service that says nothing');
    assert.ok(log.play > playsBefore);
    assert.equal(events('subtitle-translate-wait').length, 0);
});

// ---- a seek silences the run it leaves ---------------------------------

// seekerOn builds a seeker over the hls.js fake, with a POST that takes
// `postMs` and answers `ok`.
async function seekerOn(t, { postMs = 100, ok = true } = {}) {
    t.after(() => destroyPlayer());
    const p = await mountPlayer((it) => { it.installHls({ subtitleTrack: -1, subtitleDisplay: false }); });
    p.setResponse((url, params) => (params && params.method === 'POST'
        ? new Promise((resolve) => setTimeout(() => resolve({ ok, status: ok ? 200 : 503, json: async () => ({ offset: 118.5 }) }), postMs))
        : { ok: true, status: 200, json: async () => ({}) }));
    const log = playback(p.video);
    const seeker = createSessionSeeker({
        hls: window.hlsPlayer, videoEl: p.video, sessionSeekUrl: '/session/seek', sourceUrl: 'https://x.test/index.m3u8',
        trackContainer: p.container, onSeekOffsetChange: () => {}, onSeekingChange: () => {},
    });
    return { p, log, seeker };
}

test('a seek pauses the run it leaves and plays the one it starts', { timeout: 5000 }, async (t) => {
    // Owner, 2026-09-18: the old position's sound went on under the frozen
    // frame for as long as the transcoder took to start the new one.
    const { p, log, seeker } = await seekerOn(t);
    p.video.paused = false;
    const done = seeker.seek(120);
    await wait(20);
    assert.equal(p.video.paused, true, 'silent while the POST is out');
    const playsDuringPost = log.play;
    await wait(150);
    assert.ok(log.play > playsDuringPost, 'played again once the new source is loading');
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await done;
    assert.equal(seeker.isSeeking(), false);
});

test('a seek on a paused film starts nothing, unless the caller says the pause was not the viewer’s', { timeout: 5000 }, async (t) => {
    const { p, log, seeker } = await seekerOn(t, { postMs: 10 });
    seeker.seek(120);
    await wait(60);
    assert.equal(log.play, 0, 'a viewer who seeks while paused stays paused');
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await settle();

    // The resume prompt's answer: the film is held, and the seek is what
    // ends the hold.
    const done = seeker.seek(240, { play: true });
    await wait(60);
    assert.ok(log.play >= 1, 'the new run is started');
    p.video.dispatchEvent(new dom.window.Event('playing'));
    await done;
});

test('a refused seek gives the film back as it was: playing', { timeout: 5000 }, async (t) => {
    const { p, log, seeker } = await seekerOn(t, { postMs: 10, ok: false });
    const orig = console.error;
    console.error = () => {};
    t.after(() => { console.error = orig; });
    p.video.paused = false;
    await seeker.seek(120);
    assert.ok(log.pause >= 1);
    assert.equal(p.video.paused, false, 'the pause the seek made is undone with it');
});

test('pressing play during a wait is pressing Keep watching: the banner says so at once', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    playback(p.video);
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? catchUpResponse('12/400', '100')
        : { ok: true, status: 200, json: async () => ({}) }));
    p.video.paused = false;
    p.video.currentTime = 100;
    click(p.container.querySelector('#subtitles .subtitle[data-id="tr-pt"]'));
    await settle();
    assert.equal(catchUpText(p), 'player.subtitleCatchUpWaitingShort', 'held: the start hold');

    p.video.play();
    await settle();
    assert.equal(catchUpText(p), 'player.subtitleCatchUpShort', 'no longer "paused until…" over a film that is playing');
    assert.equal(catchUpBanner(p).querySelector('.wt-catchup-btn').textContent, 'player.subtitleCatchUpWait');
});

// ---- "caught up" is said, not implied -----------------------------------

test('the film brakes a second before the first untranslated line, and resumes without a rewind', async (t) => {
    // Owner, 2026-09-18: better to slow down before a line the translation
    // is not ready for than to rewind after it. The 3 s tick cannot do that
    // on its own -- the frontier can be 2.5 s ahead on one tick and behind
    // on the next -- so the brake is decided on timeupdate.
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    const log = playback(p.video);
    let pending = '130';
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? catchUpResponse('12/400', pending)
        : { ok: true, status: 200, json: async () => ({}) }));
    await pickPausedThenPlay(p, 100);
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(catchUpBanner(p), null, 'fixture: 30 s ahead, nothing to say');
    const tick = async (at) => {
        p.video.currentTime = at;
        p.video.dispatchEvent(new dom.window.Event('timeupdate'));
        await settle();
    };

    await tick(128.5);
    assert.equal(p.video.paused, false, 'a second and a half to go: still playing');
    const heads = p.calls.filter((c) => c.params && c.params.method === 'HEAD').length;
    await tick(129.2);
    assert.equal(p.video.paused, true, 'inside the last second: held before the line');
    assert.equal(catchUpText(p), 'player.subtitleCatchUpWaitingShort');
    assert.equal(p.events.filter((e) => e.name === 'subtitle-translate-wait').at(-1).data.auto, true);
    assert.ok(p.calls.filter((c) => c.params && c.params.method === 'HEAD').length > heads,
        'and the poll is asked at once: a stale frontier must cost one HEAD, not three seconds');

    pending = '400';
    const playsBefore = log.play;
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(log.play > playsBefore, 'the run got ahead: the film goes on');
    const done = p.events.filter((e) => e.name === 'subtitle-translate-wait-done').at(-1);
    assert.equal(done.data.rewind, 0, 'nothing was missed, so nothing is replayed');
    assert.equal(p.video.currentTime, 129.2);
});

test('the brake leaves alone a viewer who said they would rather watch', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    const log = playback(p.video);
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? catchUpResponse('12/400', '100')
        : { ok: true, status: 200, json: async () => ({}) }));
    await pickPausedThenPlay(p, 100);
    assert.equal(p.video.paused, true, 'fixture: held');
    click(catchUpBanner(p).querySelector('.wt-catchup-btn'));
    await settle();
    const pauses = log.pause;
    p.video.currentTime = 101;
    p.video.dispatchEvent(new dom.window.Event('timeupdate'));
    await settle();
    assert.equal(log.pause, pauses, 'Keep watching holds for the rest of this stretch');
});

test('the pill says "caught up" when the subtitles are back, and then goes', async (t) => {
    const flash = catchUpTiming.caughtUpFlashMs;
    catchUpTiming.caughtUpFlashMs = 400;
    t.after(() => { catchUpTiming.caughtUpFlashMs = flash; destroyPlayer(); });
    clearOfferMemory();
    const p = await mountPlayer();
    let pending = '100';
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? catchUpResponse('12/400', pending)
        : { ok: true, status: 200, json: async () => ({}) }));
    await pickPausedThenPlay(p, 100);
    assert.ok(behindBanner(p), 'behind first');

    pending = '400';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(behindBanner(p), null);
    assert.ok(doneBanner(p), 'the pill does not just vanish');
    assert.equal(doneBanner(p).querySelector('.wt-catchup-text').textContent, 'player.subtitleCaughtUp');
    assert.equal(doneBanner(p).querySelector('button'), null, 'nothing to press on good news');

    await wait(600);
    assert.equal(catchUpBanner(p), null, 'and then it goes');
});

test('"caught up" does not outlive the translation it is about', async (t) => {
    // Found in review (2026-09-18): the flash ignores a "nothing to show"
    // so the next tick cannot cut it short -- and ignored the viewer
    // switching the translation off as well.
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    let pending = '100';
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? catchUpResponse('12/400', pending)
        : { ok: true, status: 200, json: async () => ({}) }));
    await pickPausedThenPlay(p, 100);
    pending = '400';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(doneBanner(p), 'the flash is up');

    click(p.chip('none'));
    await settle();
    assert.equal(catchUpBanner(p), null, 'no translation, nothing to have caught up');
});

test('a dismissed banner is not followed by "caught up"', async (t) => {
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer();
    let pending = '100';
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? catchUpResponse('12/400', pending)
        : { ok: true, status: 200, json: async () => ({}) }));
    await pickPausedThenPlay(p, 100);
    click(behindBanner(p).querySelector('.wt-catchup-close'));
    await settle();
    pending = '400';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.equal(catchUpBanner(p), null, 'the viewer asked not to hear about it');
});

test('the viewer is behind when the TRACK is, whatever the service says', async (t) => {
    // Owner, 2026-09-18: "the subtitles do not keep up", with no banner to
    // say why. The service was ahead; the throttled <track> was not.
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer(null, { tracks: [['tr-pt', false]] });
    let answer = () => catchUpResponse('5/400', '130');
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? answer()
        : { ok: true, status: 200, json: async () => ({}) }));
    const trackSrc = () => p.video.querySelector('track#tr-pt').getAttribute('src') || '';
    await pickPausedThenPlay(p, 100);
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(/rev=5\b/.test(trackSrc()), `loaded up to 130 s: ${trackSrc()}`);

    // The service races ahead to 400 s; nothing reloads (15 s throttle, and
    // the viewer is 30 s short of the edge of what they have).
    answer = () => catchUpResponse('40/400', '400');
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(/rev=5\b/.test(trackSrc()), 'plenty loaded ahead: no swap yet');

    // The viewer reaches the edge of the loaded cues: that is a swap NOW.
    p.video.currentTime = 127;
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(/rev=40\b/.test(trackSrc()), `the throttle yields to a viewer running out of cues: ${trackSrc()}`);
});

test('the edge of an unshifted track is read in movie time, not pushed out by the run offset', async (t) => {
    // Found in review (2026-09-18): coverageEnd added the session offset to
    // a cue that had not been shifted yet -- one that is in movie time
    // already. In a run starting at 600 s a track ending at 650 s claimed
    // 1250 s, so the viewer ran out of cues with no swap and no banner.
    let answer = () => catchUpResponse('5/400', null);
    const { p } = await mountSessionRun(t, () => answer(), { sessionOffset: 600, startPaused: true });
    const el = p.video.querySelector('track#tr-pt');
    const trackSrc = () => el.getAttribute('src') || '';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(/rev=5\b/.test(trackSrc()), `fixture: swapped with nothing pending: ${trackSrc()}`);

    // The revision parsed, and the offset pass has not reached it yet.
    el.track.cues.length = 0;
    el.track.addCue({ startTime: 640, endTime: 650 });
    // The service is far ahead; the viewer (600 + 48) is at the track's edge.
    answer = () => catchUpResponse('40/400', '2000');
    p.video.currentTime = 48;
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(/rev=40\b/.test(trackSrc()), `the viewer is running out of cues, so the swap is now: ${trackSrc()}`);
});

test('a seek the service already covers is not held for a track that is merely stale', async (t) => {
    // Found in review (2026-09-18): the forced swap was skipped while the
    // silent pre-hold had the film paused, so the stale track edge read as
    // "behind" -- a visible wait, a banner and a "caught up" flash over cues
    // the service had all along.
    let answer = () => catchUpResponse('5/400', '130');
    const { p, log, seek, events } = await mountSessionRun(t, () => answer(), { startPaused: true });
    const el = p.video.querySelector('track#tr-pt');
    const trackSrc = () => el.getAttribute('src') || '';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(/rev=5\b/.test(trackSrc()), `fixture: loaded up to 130 s: ${trackSrc()}`);

    // The service gets far ahead and the player hears about it before the
    // seek: the swap is throttled (plenty loaded ahead of 1 s), and the
    // seek's kick will find no NEW change to force one with.
    answer = () => catchUpResponse('40/400', '2000');
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(/rev=5\b/.test(trackSrc()), `fixture: throttled, still the old revision: ${trackSrc()}`);

    // The seek lands at 210 s: past the loaded edge, well inside what the
    // service has translated. (timeupdate: the arrow key seeks from the
    // player's own clock, which jsdom does not advance.)
    // The brake is kept out of the jump itself (it skips a hidden tab; no
    // visibilitychange is sent, so nothing else notices): a film playing at
    // 200 s over a track that ends at 130 s is exactly what the brake is
    // for, and this test is about the seek.
    Object.defineProperty(document, 'hidden', { configurable: true, value: true });
    p.video.currentTime = 200;
    p.video.dispatchEvent(new dom.window.Event('timeupdate'));
    await settle();
    delete document.hidden;
    // Answers take 200 ms from here on, so the one that matters lands
    // while the silent hold has the film paused -- a mid-seek answer,
    // with the film still running, swaps on its own and hides the bug.
    const prompt = answer;
    answer = () => new Promise((resolve) => setTimeout(() => resolve(prompt()), 200));
    const playsBefore = log.play;
    await seek();
    assert.equal(p.video.paused, true, 'fixture: the silent hold is on when the answer arrives');
    await wait(500);

    assert.ok(/rev=40\b/.test(trackSrc()), `the swap happens under the silent hold: ${trackSrc()}`);
    assert.equal(events('subtitle-translate-wait').length, 0, 'no wait was ever started');
    assert.equal(behindBanner(p), null, 'and nothing was said');
    assert.ok(log.play > playsBefore, 'the silent hold lets the film go');
    assert.equal(p.video.paused, false);
});

// ---- a revision's load does not switch the translation off ---------------

test('a track switched off while its revision loads is switched back on when it settles', async (t) => {
    // The whole chain behind "no subtitles until pause -> play" (owner,
    // 2026-09-18, on OpenSubtitles and embedded sources alike): something
    // disables the element-backed track -- a session seek's loadSource does
    // -- and nothing re-asserts the picker's selection after a <track> load,
    // because the re-assertion listens to hls.js events only.
    t.after(() => destroyPlayer());
    clearOfferMemory();
    const p = await mountPlayer(null, { tracks: [['tr-pt', false]] });
    p.setResponse((url, params) => (params && params.method === 'HEAD'
        ? catchUpResponse('5/400', '400')
        : { ok: true, status: 200, json: async () => ({}) }));
    const el = p.video.querySelector('track#tr-pt');
    await pickPausedThenPlay(p, 100);
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(/rev=5\b/.test(el.getAttribute('src') || ''), 'a revision is loading');
    assert.equal(p.mode('tr-pt'), 'showing', 'fixture: the picked translation is on');

    // What hls.loadSource() does to every element-backed track.
    el.track.mode = 'disabled';
    el.dispatchEvent(new dom.window.Event('load'));
    await settle();
    assert.equal(p.mode('tr-pt'), 'showing', 'the settle writes the picker’s answer again');
});

// ---- restored cues carry the CURRENT run's offset -------------------------

test('cues a reload puts back are shifted by the run that is playing now', async (t) => {
    // The hole behind "the subtitles do not match the dialogue at all" after
    // a seek (owner, 2026-09-18): an empty revision makes the reload restore
    // the cue objects it had snapshotted, and those still carry the shift of
    // the run they were loaded in. The capture 'load' listener cannot help --
    // it runs BEFORE the restore, over an empty list.
    // Nothing translated yet, so nothing is swapped in during the mount:
    // the snapshot under test has to be taken with the stale cue in it.
    let progress = '0/400';
    const { p } = await mountSessionRun(t, () => catchUpResponse(progress, '400'), { sessionOffset: 90, startPaused: true });
    const el = p.video.querySelector('track#tr-pt');
    // Loaded while the run started at 0: movie time 100 s sat at 100 s.
    const stale = { startTime: 100, endTime: 103, __absStart: 100, __absEnd: 103 };
    el.track.cues.length = 0;
    el.track.addCue(stale);
    progress = '5/400';
    await wait(POLL_INTERVAL_WINDOW_MS);
    assert.ok(/rev=5\b/.test(el.getAttribute('src') || ''), 'fixture: a revision swap is pending');

    // The browser dropped the list while reparsing, and the revision is empty.
    el.track.cues.length = 0;
    el.dispatchEvent(new dom.window.Event('load'));
    await settle();
    assert.equal(el.track.cues.length, 1, 'the viewer keeps the line they had');
    assert.equal(el.track.cues[0].startTime, 10, 'at 100 - 90 s: the run that is playing starts at 90');
});
