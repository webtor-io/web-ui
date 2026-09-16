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
} = await import('./Player.jsx');
// Imported the same way and for the same reason as Player.jsx: hls-manager
// reads navigator at module scope, so it cannot be a static import above
// the globals.
const { Hls, initDefaultTracks } = await import('./hls-manager.js');
const { createSessionSeeker } = await import('./session-seek.js');

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
function mount({ tracks = [], hlsTracks = [] } = {}) {
    document.body.innerHTML = `
        <div id="page">
            <video class="player" data-resource-id="res" data-path="movie.mkv">
                ${tracks.map(([id, showing]) => `<track id="${id}" src="https://x.test/${id}.vtt" srclang="en" label="${id}" kind="subtitles"${showing ? ' default="default"' : ''}>`).join('')}
                ${hlsTracks.map(([label]) => `<track src="https://x.test/manifest.vtt" srclang="ru" label="${label}" kind="subtitles">`).join('')}
            </video>
            ${DIALOG}
        </div>`;
    const container = document.getElementById('page');
    const video = container.querySelector('video.player');
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
        installHls: (init) => { window.hlsPlayer = makeHls(init); return window.hlsPlayer; },
        hlsTrack: (i) => hlsTrackEls[i].track,
        hlsModes: () => hlsTrackEls.map((el) => el.track.mode),
        mode: (id) => {
            const t = video.textTracks.find((tt) => tt.id === id);
            return t ? t.mode : null;
        },
    };
}

// makeHls is hls.js as this code touches it: two properties that decide
// what it draws, and the event bus it announces its own transitions on.
// Everything it does to the TextTracks in reaction (toggleTrackModes) is
// driven by hand in the tests that need it — writing a second
// implementation of hls.js here would only prove that the copy agrees with
// itself.
function makeHls({ subtitleTrack = -1, subtitleDisplay = true } = {}) {
    const listeners = new Map();
    const writes = [];
    let t = subtitleTrack;
    let d = subtitleDisplay;
    const add = (ev, fn) => listeners.set(ev, [...(listeners.get(ev) || []), fn]);
    return {
        writes,
        audioTrack: -1,
        get subtitleTrack() { return t; },
        set subtitleTrack(v) { t = v; writes.push(['subtitleTrack', v]); },
        get subtitleDisplay() { return d; },
        set subtitleDisplay(v) { d = v; writes.push(['subtitleDisplay', v]); },
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
        emit: (ev, data) => { for (const fn of [...(listeners.get(ev) || [])]) fn(ev, data); },
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
    assert.equal(heads[0].url, src);

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
    const hls = makeHls({ subtitleTrack: -1, subtitleDisplay: true });
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

test('a seek re-applies the picker’s current answer, not the one it started with', async () => {
    // The subtitles dialog stays open and clickable while a seek is in
    // flight (the POST, the manifest reload and the buffering after it are
    // seconds), so the selection that was playing when the seek began is
    // not necessarily the one to come back to. It used to be restored from
    // a snapshot taken at seek start — which, after a switch to a
    // side-loaded track, put hls.js back on the embedded track and left
    // both on screen.
    const p = mount({ hlsTracks: [['Full (rus)', 'showing']] });
    const hls = p.installHls({ subtitleTrack: 0, subtitleDisplay: true });
    p.wire();
    // Playing at seek time: the embedded Russian track.
    p.chip('none').removeAttribute('data-default');
    p.chip('mp-0').setAttribute('data-default', 'true');
    p.modal.setAttribute('data-subtitles-off', 'false');

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
    p.mode('os-os-ru');
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
});
