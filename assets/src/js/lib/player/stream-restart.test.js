import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import {
    AUTO_RESTART_EVERY_MS, BUDGET_KEY, NOTE_KEY, NOTE_TTL_MS, PURGE_MARK,
    canAutoRestart, recordRestart, writeNote, takeNote, restartURL, restartStream, clearPurgeMarks, markNextStart,
    createRecoveryPolicy, fileKey,
} from './stream-restart.js';

function page({ url = 'https://webtor.io/ru/res1?file=a.mkv', storageThrows = false } = {}) {
    const dom = new JSDOM('<!doctype html><body></body>', { url, pretendToBeVisual: true });
    const win = dom.window;
    if (storageThrows) {
        Object.defineProperty(win, 'sessionStorage', { get() { throw new win.DOMException('denied', 'SecurityError'); }, configurable: true });
    }
    return win;
}

// A Storage whose writes fail (a full or blocked one that still reads).
function readOnlyStorage(win) {
    const s = win.sessionStorage;
    return { getItem: (k) => s.getItem(k), setItem: () => { throw new win.DOMException('quota', 'QuotaExceededError'); }, removeItem: (k) => s.removeItem(k) };
}

// ---- the budget ------------------------------------------------------------

test('budget: one automatic restart per file per 5 minutes, per tab', () => {
    const s = page().sessionStorage;
    const a = fileKey('res1', 'a.mkv');
    const b = fileKey('res1', 'b.mkv');
    assert.equal(canAutoRestart(s, a, 1000), true);
    assert.equal(recordRestart(s, a, 1000), true);
    assert.equal(canAutoRestart(s, a, 1000 + AUTO_RESTART_EVERY_MS - 1), false);
    assert.equal(canAutoRestart(s, b, 2000), true, 'another file has its own');
    assert.equal(canAutoRestart(s, a, 1000 + AUTO_RESTART_EVERY_MS), true);
    // Old entries go on the next write.
    recordRestart(s, b, 1000 + AUTO_RESTART_EVERY_MS + 5);
    assert.deepEqual(Object.keys(JSON.parse(s.getItem(BUDGET_KEY))), [b]);
    assert.equal(AUTO_RESTART_EVERY_MS, 5 * 60 * 1000);
});

test('budget: where it cannot be read or written, no automatic restart at all', () => {
    const win = page();
    assert.equal(canAutoRestart(null, 'k', 0), false);
    assert.equal(recordRestart(null, 'k', 0), false);
    assert.equal(recordRestart(readOnlyStorage(win), 'k', 0), false);
    win.sessionStorage.setItem(BUDGET_KEY, '{not json');
    assert.equal(canAutoRestart(win.sessionStorage, 'k', 0), false);
    assert.equal(recordRestart(win.sessionStorage, 'k', 0), true, 'a broken entry is overwritten');
});

// ---- the note --------------------------------------------------------------

test('note: the next player of the same file takes it once, fresh', () => {
    const s = page().sessionStorage;
    writeNote(s, { resourceID: 'res1', path: 'a.mkv', at: 1234.5, play: true }, 0);
    assert.equal(takeNote(s, 'res1', 'b.mkv', 10), null, 'another file does not take it');
    assert.ok(s.getItem(NOTE_KEY), 'and leaves it');
    assert.deepEqual(takeNote(s, 'res1', 'a.mkv', 10), { at: 1234.5, play: true, grace: '' });
    assert.equal(takeNote(s, 'res1', 'a.mkv', 10), null, 'once');
    writeNote(s, { resourceID: 'res1', path: 'a.mkv', at: 60, play: false }, 0);
    assert.equal(takeNote(s, 'res1', 'a.mkv', NOTE_TTL_MS + 1), null, 'stale');
    assert.equal(s.getItem(NOTE_KEY), null, 'and gone');
    assert.equal(takeNote(null, 'res1', 'a.mkv'), null);
});

// The grace popup's answer rides along: the restarted player does not ask
// the question again (Player.jsx). Only the popup's own answers.
test('note: it carries the grace popup\'s answer', () => {
    const s = page().sessionStorage;
    writeNote(s, { resourceID: 'res1', path: 'a.mkv', at: 150, play: false, grace: 'continue' }, 0);
    assert.deepEqual(takeNote(s, 'res1', 'a.mkv', 10), { at: 150, play: false, grace: 'continue' });
    writeNote(s, { resourceID: 'res1', path: 'a.mkv', at: 150, play: false, grace: 'dismiss' }, 0);
    assert.equal(takeNote(s, 'res1', 'a.mkv', 10).grace, 'dismiss');
    writeNote(s, { resourceID: 'res1', path: 'a.mkv', at: 150, play: false, grace: '<b>' }, 0);
    assert.equal(takeNote(s, 'res1', 'a.mkv', 10).grace, '');
    s.setItem(NOTE_KEY, JSON.stringify({ resourceID: 'res1', path: 'a.mkv', at: 150, play: false, grace: 'x', until: 99 }));
    assert.equal(takeNote(s, 'res1', 'a.mkv', 10).grace, '', 'what the page reads back is checked too');
});

test('restartURL: this file, started at once, past the job cache', () => {
    assert.equal(restartURL('https://webtor.io/ru/res1?file-idx=3&x=1#old', 'S01/ep 2.mkv'),
        '/ru/res1?x=1&file=S01%2Fep+2.mkv#action=stream&purge=true');
});

// ---- the restart -----------------------------------------------------------

function startForm(win, { rid = 'res1', iid = 'item1', endpoint = 'stream-video' } = {}) {
    const f = win.document.createElement('form');
    f.setAttribute('action', `/ru/${endpoint}`);
    f.setAttribute('method', 'post');
    f.className = endpoint;
    f.innerHTML = `<input type="hidden" name="resource-id" value="${rid}"><input type="hidden" name="item-id" value="${iid}">`;
    win.document.body.appendChild(f);
    return f;
}

function captureSubmits(win) {
    const got = [];
    win.document.addEventListener('submit', (e) => {
        e.preventDefault();
        got.push(Object.fromEntries(new win.FormData(e.target)));
    });
    return got;
}

// The player's element and its #subtitles dialog, as the video render has
// them: data-item-id only on the dialog (the element carries it only for a
// passthrough or a multichannel-audio start).
function player(win, { rid = 'res1', iid = 'item1', path = 'a.mkv', decode = '', onElement = false } = {}) {
    const root = win.document.createElement('div');
    root.innerHTML = `<dialog id="subtitles" data-resource-id="${rid}" data-item-id="${iid}"></dialog>`;
    const v = win.document.createElement('video');
    v.dataset.resourceId = rid;
    v.dataset.path = path;
    if (decode) v.dataset.decode = decode;
    if (onElement) v.dataset.itemId = iid;
    root.appendChild(v);
    win.document.body.appendChild(root);
    return { v, root };
}

test('restart on the resource page: this file\'s start form again, with purge, once marked', () => {
    const win = page();
    const other = startForm(win, { iid: 'item0' });
    startForm(win);
    const submits = captureSubmits(win);
    const p = player(win);
    const how = restartStream({ win, doc: win.document, video: p.v, root: p.root, navigate: () => assert.fail('no navigation') });
    assert.equal(how, 'form');
    assert.equal(submits.length, 1);
    assert.equal(submits[0]['item-id'], 'item1');
    assert.equal(submits[0].purge, 'true', 'past the job cache: a replay hands back the dead session');
    assert.equal(other.querySelector('input[name="purge"]'), null);
    // Twice (the card's click after an automatic one): one field.
    restartStream({ win, doc: win.document, video: p.v, root: p.root });
    assert.equal(win.document.querySelectorAll('input[name="purge"]').length, 1);
    clearPurgeMarks(win.document);
    assert.equal(win.document.querySelectorAll(`input[${PURGE_MARK}]`).length, 0, 'the next player takes it off');
});

test('restart: the item on the element wins; an audio render (no dialog, no item) goes by the deep link', () => {
    const win = page();
    startForm(win, { iid: 'item1', endpoint: 'stream-audio' });
    const submits = captureSubmits(win);
    const a = player(win, { iid: 'item1', onElement: true });
    a.root.querySelector('#subtitles').remove();
    assert.equal(restartStream({ win, doc: win.document, video: a.v, root: a.root }), 'form');
    assert.equal(submits.length, 1);
    const win2 = page();
    startForm(win2, { iid: 'item1', endpoint: 'stream-audio' });
    const v = win2.document.createElement('audio');
    v.dataset.resourceId = 'res1';
    v.dataset.path = 'music/01.flac';
    win2.document.body.appendChild(v);
    const went = [];
    assert.equal(restartStream({ win: win2, doc: win2.document, video: v, root: null, navigate: (u) => went.push(u) }), 'navigate');
    assert.deepEqual(went, ['/ru/res1?file=music%2F01.flac#action=stream&purge=true']);
});

test('restart after a quiet move to the next file: this file by the deep link, not the previous one\'s form', () => {
    const win = page();
    startForm(win, { iid: 'ep1' });
    const submits = captureSubmits(win);
    const p = player(win, { iid: 'ep2', path: 'S01/ep2.mkv' });
    const went = [];
    assert.equal(restartStream({ win, doc: win.document, video: p.v, root: p.root, navigate: (u) => went.push(u) }), 'navigate');
    assert.equal(submits.length, 0);
    assert.deepEqual(went, ['/ru/res1?file=S01%2Fep2.mkv#action=stream&purge=true']);
});

// The viewer answered the slow-download modal "watch as is" for this run
// (the job renders data-offer-answered for a force-slow start): the restart
// carries that answer, or the job asks the same question before any player.
test('restart after "watch as is": force-slow with the purge, on the form and on the deep link', () => {
    const win = page();
    startForm(win);
    const submits = captureSubmits(win);
    const p = player(win);
    p.v.dataset.offerAnswered = 'continue-slow';
    assert.equal(restartStream({ win, doc: win.document, video: p.v, root: p.root }), 'form');
    assert.equal(submits[0].purge, 'true');
    assert.equal(submits[0]['force-slow'], 'true');
    clearPurgeMarks(win.document);
    assert.equal(win.document.querySelector('input[name="force-slow"]'), null, 'the next player takes it off too');
    // Without the answer: no force-slow.
    const q = player(win);
    restartStream({ win, doc: win.document, video: q.v, root: q.root });
    assert.equal(submits[1]['force-slow'], undefined);
    // Another file's form: the deep link carries it.
    const win2 = page();
    startForm(win2, { iid: 'ep1' });
    const r = player(win2, { iid: 'ep2', path: 'S01/ep2.mkv' });
    r.v.dataset.offerAnswered = 'continue-slow';
    const went = [];
    restartStream({ win: win2, doc: win2.document, video: r.v, root: r.root, navigate: (u) => went.push(u) });
    assert.deepEqual(went, ['/ru/res1?file=S01%2Fep2.mkv#action=stream&purge=true&force-slow=true']);
});

test('restart in an embed: its POST again, the declaration it started with, and purge', () => {
    const win = page({ url: 'https://webtor.io/embed?id=e1' });
    win._embedSettings = { magnet: 'x', lang: 'en' };
    win._CSRF = 'csrf';
    win._sessionID = 'sid';
    const posted = [];
    win.HTMLFormElement.prototype.submit = function () { posted.push(Object.fromEntries(new win.FormData(this))); };
    const p = player(win, { decode: 'hevc8,aac51' });
    assert.equal(restartStream({ win, doc: win.document, video: p.v, root: p.root }), 'embed');
    assert.deepEqual(posted, [{ _csrf: 'csrf', _sessionID: 'sid', settings: JSON.stringify({ magnet: 'x', lang: 'en' }), decode: 'hevc8,aac51', purge: 'true' }]);
    const q = player(win);
    restartStream({ win, doc: win.document, video: q.v, root: q.root });
    assert.equal(posted[1].decode, undefined, 'no declaration: none');
});

// The recovery gave up (the card): the viewer's own press of the page's
// button next replayed this job, dead session and all (Chrome, 2026-09-30,
// 9c). The form is marked now, nothing is started, and the next start from
// it -- the card's or the button's -- goes past the job cache, once.
test('the card: this file\'s form marked for the next start -- purge, force-slow where answered -- nothing started', () => {
    const win = page();
    const other = startForm(win, { iid: 'item0' });
    const form = startForm(win);
    const submits = captureSubmits(win);
    const p = player(win);
    p.v.dataset.offerAnswered = 'continue-slow';
    assert.equal(markNextStart({ win, doc: win.document, video: p.v, root: p.root }), form);
    assert.equal(submits.length, 0, 'nothing started');
    assert.equal(other.querySelector('input[name="purge"]'), null, 'another file\'s form is left alone');
    // The viewer's press of "Смотреть".
    form.requestSubmit();
    assert.equal(submits[0].purge, 'true');
    assert.equal(submits[0]['force-slow'], 'true');
    // The card's own click after it: one field each.
    markNextStart({ win, doc: win.document, video: p.v, root: p.root });
    restartStream({ win, doc: win.document, video: p.v, root: p.root });
    assert.equal(win.document.querySelectorAll('input[name="purge"]').length, 1);
    assert.equal(win.document.querySelectorAll('input[name="force-slow"]').length, 1);
    // Once: the next player (or the card coming down) takes them off.
    clearPurgeMarks(win.document);
    form.requestSubmit();
    assert.equal(submits.at(-1).purge, undefined);
    assert.equal(submits.at(-1)['force-slow'], undefined);
    // Without the answer: the purge only.
    const q = player(win);
    markNextStart({ win, doc: win.document, video: q.v, root: q.root });
    assert.equal(form.querySelector('input[name="purge"]').hasAttribute(PURGE_MARK), true);
    assert.equal(form.querySelector('input[name="force-slow"]'), null);
});

test('the card: no form of this file (another file\'s after a quiet move, an embed) -> nothing marked', () => {
    const win = page();
    startForm(win, { iid: 'ep1' });
    const p = player(win, { iid: 'ep2', path: 'S01/ep2.mkv' });
    assert.equal(markNextStart({ win, doc: win.document, video: p.v, root: p.root }), null);
    assert.equal(win.document.querySelector('input[name="purge"]'), null);
    const win2 = page({ url: 'https://webtor.io/embed?id=e1' });
    win2._embedSettings = { magnet: 'x' };
    startForm(win2);
    const q = player(win2);
    assert.equal(markNextStart({ win: win2, doc: win2.document, video: q.v, root: q.root }), null);
    assert.equal(win2.document.querySelector('input[name="purge"]'), null);
});

// ---- the policy ------------------------------------------------------------

// el: the element as the policy asks after it -- playing from its buffer or
// not; starve() is its `waiting` (or the viewer's pause).
function policy({ storage, t = { now: 0 }, grace = { blocks: false }, leaving = { on: false }, pos = { at: 1234, play: true },
    block = { on: false }, el = { buffered: false } } = {}) {
    const events = [];
    const restarts = [];
    const cards = [];
    let stops = 0;
    let hides = 0;
    const watchers = new Set();
    const p = createRecoveryPolicy({
        storage, key: 'res1:a.mkv', now: () => t.now,
        graceBlocks: (at) => grace.blocks && at >= 0,
        leaving: () => leaving.on,
        blocked: () => block.on,
        playingFromBuffer: () => el.buffered,
        whenStarving: (fn) => { watchers.add(fn); return () => watchers.delete(fn); },
        position: () => ({ ...pos }),
        restart: (x) => restarts.push(x),
        stopLoad: () => { stops++; },
        showCard: (c) => cards.push(c),
        hideCard: () => { hides++; },
        track: (name, data) => events.push({ name, data }),
    });
    const starve = () => { el.buffered = false; for (const fn of [...watchers]) fn(); };
    return { p, events, restarts, cards, stops: () => stops, hides: () => hides, starve, watching: () => watchers.size };
}

test('policy: the session gone -> the restart at the viewer\'s place, once', () => {
    const storage = page().sessionStorage;
    const x = policy({ storage });
    x.p.sessionGone('404', 404);
    x.p.sessionGone('404', 404);
    x.p.giveUp(500);
    assert.deepEqual(x.restarts, [{ at: 1234, play: true }]);
    assert.deepEqual(x.events, [{ name: 'player-recover-restart', data: { reason: '404', status: 404, via: 'load' } }]);
    assert.deepEqual(x.cards, []);
});

test('policy: a seek\'s target is where it restarts', () => {
    const x = policy({ storage: page().sessionStorage });
    x.p.sessionGone('403', 403, { at: 3000, play: false }, 'seek');
    assert.deepEqual(x.restarts, [{ at: 3000, play: false }]);
    assert.deepEqual(x.events[0].data, { reason: '403', status: 403, via: 'seek' });
});

test('policy: once per 5 minutes, then the card; its click restarts, playing; later, automatic again', () => {
    const storage = page().sessionStorage;
    const t = { now: 0 };
    policy({ storage, t }).p.sessionGone('404', 404);
    // The next page (the restarted player), its session dead again at once.
    t.now = 60 * 1000;
    const x = policy({ storage, t, pos: { at: 1300, play: false } });
    x.p.sessionGone('404', 404);
    assert.deepEqual(x.restarts, []);
    assert.deepEqual(x.cards, [{ reason: 'limit', status: 404 }]);
    assert.equal(x.stops(), 1, 'loading stopped under the card');
    assert.deepEqual(x.events, [{ name: 'player-recover-card-shown', data: { reason: 'limit', status: 404, cause: '404', via: 'load' } }]);
    x.p.sessionGone('404', 404);
    assert.equal(x.cards.length, 1, 'one card');
    x.p.click();
    assert.deepEqual(x.restarts, [{ at: 1300, play: true }], '"Continue watching" is a Play');
    assert.deepEqual(x.events[1], { name: 'player-recover-card-click', data: { reason: 'limit', status: 404 } });
    // The click counted: another death within 5 min of it is the card again.
    t.now += AUTO_RESTART_EVERY_MS - 1;
    const y = policy({ storage, t });
    y.p.sessionGone('403', 403);
    assert.equal(y.cards.length, 1);
    t.now += 1;
    const z = policy({ storage, t });
    z.p.sessionGone('403', 403);
    assert.equal(z.restarts.length, 1, 'five minutes on: automatic again');
});

test('policy: storage blocked -> no automatic restart, the card at once', () => {
    const x = policy({ storage: null });
    x.p.sessionGone('404', 404);
    assert.deepEqual(x.restarts, []);
    assert.deepEqual(x.cards, [{ reason: 'limit', status: 404 }]);
    x.p.click();
    assert.equal(x.restarts.length, 1, 'the click still restarts');
});

test('policy: the grace popup up or on its way -> nothing until its answer', () => {
    const storage = page().sessionStorage;
    const grace = { blocks: true };
    const x = policy({ storage, grace });
    x.p.sessionGone('404', 404);
    assert.deepEqual(x.restarts, []);
    assert.deepEqual(x.cards, []);
    assert.equal(x.stops(), 1, 'nothing loads while it waits');
    assert.equal(canAutoRestart(storage, 'res1:a.mkv', 0), true, 'the budget is not spent on a wait');
    x.p.sessionGone('404', 404);
    grace.blocks = false;
    x.p.graceAnswered();
    assert.deepEqual(x.restarts, [{ at: 1234, play: true }]);
    x.p.graceAnswered();
    assert.equal(x.restarts.length, 1);
});

test('policy: the next file loading -> this one is not restarted', () => {
    const x = policy({ storage: page().sessionStorage, leaving: { on: true } });
    x.p.sessionGone('404', 404);
    x.p.giveUp(502);
    assert.deepEqual(x.restarts, []);
    assert.deepEqual(x.cards, []);
});

test('policy: another 4xx, or the backoff run out -> the card, reason network', () => {
    const x = policy({ storage: page().sessionStorage });
    x.p.giveUp(410);
    assert.deepEqual(x.cards, [{ reason: 'network', status: 410 }]);
    assert.deepEqual(x.events, [{ name: 'player-recover-card-shown', data: { reason: 'network', status: 410 } }]);
    x.p.click();
    assert.equal(x.restarts.length, 1);
    assert.deepEqual(x.events[1].data, { reason: 'network', status: 410 });
});

test('policy: disposed (the player went) -> nothing', () => {
    const x = policy({ storage: page().sessionStorage, grace: { blocks: true } });
    x.p.sessionGone('404', 404);
    x.p.dispose();
    x.p.graceAnswered();
    x.p.sessionGone('404', 404);
    assert.deepEqual(x.restarts, []);
});

// Under the card loading can start again -- a stall's startLoad, Play from
// the keyboard -- and the film play: the card comes down, and the next
// failure is judged afresh instead of being swallowed by a card nobody sees
// the point of.
test('policy: the film loads again under the card -> the card comes down; the next dead session restarts', () => {
    const t = { now: 0 };
    const x = policy({ storage: page().sessionStorage, t });
    x.p.resumed();
    assert.equal(x.hides(), 0, 'no card, nothing to hide');
    x.p.giveUp(0);
    assert.equal(x.cards.length, 1);
    x.p.resumed();
    assert.equal(x.hides(), 1);
    assert.equal(x.p.card, null);
    x.p.resumed();
    assert.equal(x.hides(), 1, 'once');
    x.p.sessionGone('404', 404);
    assert.deepEqual(x.restarts, [{ at: 1234, play: true }], 'no longer swallowed by the card');
    // Restarting: the page is on its way to the next player.
    x.p.resumed();
    assert.equal(x.hides(), 1);
});

test('policy: the budget\'s card comes down too; within the five minutes the next dead session is the card again', () => {
    const t = { now: 0 };
    const storage = page().sessionStorage;
    const x = policy({ storage, t });
    x.p.sessionGone('404', 404);
    const y = policy({ storage, t });
    t.now = 60 * 1000;
    y.p.sessionGone('404', 404);
    assert.deepEqual(y.cards, [{ reason: 'limit', status: 404 }]);
    y.p.resumed();
    assert.equal(y.hides(), 1);
    y.p.sessionGone('404', 404);
    assert.equal(y.cards.length, 2, 'within the five minutes: the card again, not a loop');
    assert.deepEqual(y.restarts, []);
});

// ---- the card waits for the buffer ------------------------------------------
//
// A film still playing from its buffer is not stopped: the card (limit or
// network) waits until the element starves or the viewer pauses. Chrome,
// 2026-09-30: the 429 give-up paused a film at readyState 4 just as the
// refusals stopped, and the card stayed.

test('policy: the card waits while the film plays from its buffer, and comes when it starves', () => {
    const el = { buffered: true };
    const x = policy({ storage: page().sessionStorage, el });
    x.p.giveUp(429);
    assert.deepEqual(x.cards, [], 'not over a film that plays');
    assert.equal(x.stops(), 0, 'nor is its loading stopped');
    assert.deepEqual(x.events, [], 'nothing counted yet');
    assert.ok(x.p.waiting);
    assert.equal(x.p.engaged, true);
    x.p.giveUp(429);
    assert.equal(x.watching(), 1, 'one wait');
    x.starve();
    assert.deepEqual(x.cards, [{ reason: 'network', status: 429 }]);
    assert.equal(x.stops(), 1);
    assert.deepEqual(x.events, [{ name: 'player-recover-card-shown', data: { reason: 'network', status: 429 } }]);
    assert.equal(x.watching(), 0, 'the wait is over');
});

test('policy: the budget\'s card waits too; a restart within it does not', () => {
    const storage = page().sessionStorage;
    const el = { buffered: true };
    const x = policy({ storage, el });
    x.p.sessionGone('404', 404, null, 'error-nonfatal', 'video');
    assert.deepEqual(x.restarts, [{ at: 1234, play: true }], 'the restart goes at once');
    assert.deepEqual(x.events[0].data, { reason: '404', status: 404, via: 'error-nonfatal', loader: 'video' });
    const y = policy({ storage, el });
    y.p.sessionGone('404', 404, null, 'error-nonfatal', 'audio');
    assert.deepEqual(y.cards, []);
    y.starve();
    assert.deepEqual(y.cards, [{ reason: 'limit', status: 404 }]);
    assert.deepEqual(y.events, [{ name: 'player-recover-card-shown', data: { reason: 'limit', status: 404, cause: '404', via: 'error-nonfatal' } }]);
});

test('policy: a waiting card is dropped when the film loads again, or when a restart goes', () => {
    const storage = page().sessionStorage;
    const x = policy({ storage, el: { buffered: true } });
    x.p.giveUp(502);
    x.p.resumed();
    assert.equal(x.p.waiting, null);
    assert.equal(x.watching(), 0);
    x.starve();
    assert.deepEqual(x.cards, [], 'nothing to show: the stream went on');
    assert.equal(x.hides(), 0, 'and nothing was up');
    const y = policy({ storage, el: { buffered: true } });
    y.p.giveUp(502);
    y.p.sessionGone('404', 404);
    assert.equal(y.restarts.length, 1);
    y.starve();
    assert.deepEqual(y.cards, [], 'the restart has the player');
});

// ---- where the viewer asked to be --------------------------------------------
//
// A session seek that finds the session gone moves neither the element nor
// the run's offset (session-seek.js throws before either), so position() is
// still where the viewer was before it. Its target is where they are: the
// card's click restarts there, whether the seek opened the card, came while
// it waited or while it was up. Review F2 (2026-09-30): past the budget the
// card dropped it, and the click restarted at the old place -- on a
// restarted player whose resume seek found its session gone, at the
// element's ~0.

// spent: the budget of res1:a.mkv spent a minute before t.now.
function spent(storage, t) {
    recordRestart(storage, 'res1:a.mkv', t.now - 60 * 1000);
}

test('policy: past the budget, a seek that found the session gone -> the card; its click restarts at the seek\'s target', () => {
    const storage = page().sessionStorage;
    const t = { now: 10 * 60 * 1000 };
    spent(storage, t);
    const x = policy({ storage, t });
    x.p.sessionGone('404', 404, { at: 3000, play: false }, 'seek');
    assert.deepEqual(x.restarts, []);
    assert.deepEqual(x.cards, [{ reason: 'limit', status: 404 }]);
    assert.deepEqual(x.events, [{ name: 'player-recover-card-shown', data: { reason: 'limit', status: 404, cause: '404', via: 'seek' } }]);
    x.p.click();
    assert.deepEqual(x.restarts, [{ at: 3000, play: true }], 'the target, not the element\'s 1234');
});

test('policy: a seek that found the session gone while the card waits: its target -- and the card at once, the seek having paused the film', () => {
    const storage = page().sessionStorage;
    const t = { now: 10 * 60 * 1000 };
    spent(storage, t);
    const el = { buffered: true };
    const x = policy({ storage, t, el });
    x.p.giveUp(429);
    assert.deepEqual(x.cards, [], 'waiting');
    // The seek paused the old run itself, while it was out (sessionSeeking:
    // no starving then), and a seek that found the session gone never plays
    // it again -- no event of the element's follows its answer.
    el.buffered = false;
    x.p.sessionGone('404', 404, { at: 3000, play: true }, 'seek');
    assert.deepEqual(x.cards, [{ reason: 'network', status: 429 }], 'the card that waited, now');
    assert.equal(x.watching(), 0, 'the wait is over');
    x.p.click();
    assert.deepEqual(x.restarts, [{ at: 3000, play: true }]);
    // Still playing from its buffer (a seek of another kind cannot report
    // this, but the rule is the buffer's): the card keeps waiting, the
    // target kept.
    const y = policy({ storage, t, el: { buffered: true } });
    y.p.giveUp(429);
    y.p.sessionGone('404', 404, { at: 3000, play: true }, 'seek');
    assert.deepEqual(y.cards, []);
    y.starve();
    y.p.click();
    assert.deepEqual(y.restarts, [{ at: 3000, play: true }]);
});

test('policy: a seek that found the session gone under the card: swallowed, but its target is where the click goes', () => {
    const x = policy({ storage: page().sessionStorage });
    x.p.giveUp(410);
    x.p.sessionGone('404', 404, { at: 3000, play: false }, 'seek');
    assert.equal(x.cards.length, 1, 'the one card');
    assert.deepEqual(x.restarts, []);
    x.p.click();
    assert.deepEqual(x.restarts, [{ at: 3000, play: true }]);
});

test('policy: the viewer seeks again, or the film loads again -> the element\'s place is theirs again', () => {
    const storage = page().sessionStorage;
    const x = policy({ storage });
    x.p.giveUp(410);
    x.p.sessionGone('404', 404, { at: 3000, play: false }, 'seek');
    // A local seek under the card (inside the run: no POST, nothing to
    // report): the element moved, and it is where the viewer asked.
    x.p.seeking();
    x.p.click();
    assert.deepEqual(x.restarts, [{ at: 1234, play: true }]);
    const y = policy({ storage: page().sessionStorage });
    y.p.giveUp(410);
    y.p.sessionGone('404', 404, { at: 3000, play: false }, 'seek');
    y.p.resumed();
    y.p.giveUp(410);
    y.p.click();
    assert.deepEqual(y.restarts, [{ at: 1234, play: true }], 'the card that came down took the target with it');
});

test('policy: a restart that waited for the grace popup goes to the seek\'s target, whatever reported after it', () => {
    const grace = { blocks: true };
    const x = policy({ storage: page().sessionStorage, grace });
    x.p.sessionGone('404', 404, { at: 3000, play: true }, 'seek');
    // Loading went on after all (a stall's startLoad) and a fragment of the
    // dead run was refused: its report carries no place.
    x.p.sessionGone('404', 404, null, 'error-nonfatal', 'video');
    grace.blocks = false;
    x.p.graceAnswered();
    assert.deepEqual(x.restarts, [{ at: 3000, play: true }]);
    // A seek of the viewer's while the popup was up: their place again.
    const g2 = { blocks: true };
    const y = policy({ storage: page().sessionStorage, grace: g2 });
    y.p.sessionGone('404', 404, { at: 3000, play: true }, 'seek');
    y.p.seeking();
    g2.blocks = false;
    y.p.graceAnswered();
    assert.deepEqual(y.restarts, [{ at: 1234, play: true }]);
});

// ---- one owner of what the viewer sees ---------------------------------------

test('policy: blocked (a guard\'s restart, player-dead\'s verdict) -> no restart, no card', () => {
    const storage = page().sessionStorage;
    const block = { on: true };
    const x = policy({ storage, block });
    x.p.sessionGone('404', 404);
    x.p.giveUp(410);
    assert.deepEqual(x.restarts, []);
    assert.deepEqual(x.cards, []);
    assert.deepEqual(x.events, []);
    assert.equal(canAutoRestart(storage, 'res1:a.mkv', 0), true, 'no budget spent');
    assert.equal(x.p.engaged, false);
    // A card up before the other owner took over: its button does nothing.
    block.on = false;
    x.p.giveUp(410);
    assert.equal(x.cards.length, 1);
    block.on = true;
    x.p.click();
    assert.deepEqual(x.restarts, []);
    // A card waiting for the buffer when the other owner takes over.
    const el = { buffered: true };
    const b2 = { on: false };
    const y = policy({ storage, el, block: b2 });
    y.p.giveUp(429);
    b2.on = true;
    y.starve();
    assert.deepEqual(y.cards, []);
});

test('policy: engaged -- a restart gone, a card up or waiting, a restart waiting for the grace popup', () => {
    const storage = page().sessionStorage;
    const a = policy({ storage });
    assert.equal(a.p.engaged, false);
    a.p.sessionGone('404', 404);
    assert.equal(a.p.engaged, true, 'restarting');
    const b = policy({ storage: page().sessionStorage, grace: { blocks: true } });
    b.p.sessionGone('404', 404);
    assert.equal(b.p.engaged, true, 'waiting for the popup');
    const c = policy({ storage: page().sessionStorage });
    c.p.giveUp(410);
    assert.equal(c.p.engaged, true, 'the card');
    c.p.resumed();
    assert.equal(c.p.engaged, false);
});
