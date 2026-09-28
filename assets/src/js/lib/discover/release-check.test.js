import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import { RELEASE_KEY, RELEASE_TTL_MS, rememberRelease, takeRelease, reportReleaseCheck } from './release-check.js';

const HASH = '0123456789abcdef0123456789abcdef01234567';

function page({ storageThrows = false } = {}) {
    const win = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' }).window;
    if (storageThrows) {
        Object.defineProperty(win, 'sessionStorage', { get() { throw new win.DOMException('denied', 'SecurityError'); }, configurable: true });
    }
    const events = [];
    win.umami = { track: (name, data) => events.push({ name, data }) };
    return { win, events };
}

function video(win, attrs) {
    const v = win.document.createElement('video');
    for (const [k, val] of Object.entries(attrs)) v.setAttribute(k, val);
    return v;
}

const DV5 = { codec: 'hevc', hdr: 'dv', dv5: true };

test('a release opened from Discover is compared with the file once, on its own page', () => {
    const { win, events } = page();
    const now = 1_000_000;
    rememberRelease(win, HASH.toUpperCase(), DV5, true, now);
    const v = video(win, { 'data-resource-id': HASH, 'data-video-route': 'reencode', 'data-route-reason': 'dv5' });
    assert.equal(reportReleaseCheck(win, v, 'hevc', now + 60_000), true);
    assert.deepEqual(events, [{ name: 'discover-release-check', data: {
        rel_codec: 'hevc', rel_hdr: 'dv', rel_dv5: true, rel_uhd: true, src: 'hevc', route: 'reencode', reason: 'dv5',
    } }]);
    assert.equal(win.sessionStorage.getItem(RELEASE_KEY), null, 'taken');
    assert.equal(reportReleaseCheck(win, v, 'hevc', now + 61_000), false, 'once');
});

test('another release\'s record is left for its own page; an old one is dropped', () => {
    const { win, events } = page();
    const now = 1_000_000;
    rememberRelease(win, HASH, DV5, false, now);
    const other = video(win, { 'data-resource-id': 'ffff' + HASH.slice(4) });
    assert.equal(reportReleaseCheck(win, other, 'h264', now), false);
    assert.notEqual(win.sessionStorage.getItem(RELEASE_KEY), null, 'still there for its release');
    const mine = video(win, { 'data-resource-id': HASH });
    assert.equal(reportReleaseCheck(win, mine, 'hevc', now + RELEASE_TTL_MS + 1), false, 'too old');
    assert.equal(win.sessionStorage.getItem(RELEASE_KEY), null);
    assert.deepEqual(events, []);
});

// The player reads this from an effect: a throw would take the effects
// after it down with it.
test('a throwing sessionStorage: nothing recorded, nothing sent, no exception', () => {
    const { win, events } = page({ storageThrows: true });
    assert.doesNotThrow(() => rememberRelease(win, HASH, DV5, true));
    assert.equal(takeRelease(win, HASH), null);
    assert.equal(reportReleaseCheck(win, video(win, { 'data-resource-id': HASH }), 'hevc'), false);
    assert.deepEqual(events, []);
});

test('analytics that throw: no exception out of the check', () => {
    const { win } = page();
    rememberRelease(win, HASH, DV5, false);
    win.umami = { track: () => { throw new Error('blocked'); } };
    let sent;
    assert.doesNotThrow(() => { sent = reportReleaseCheck(win, video(win, { 'data-resource-id': HASH }), 'hevc'); });
    assert.equal(sent, false);
});

test('no analytics on the page: the record stays for a page that has them', () => {
    const { win } = page();
    rememberRelease(win, HASH, DV5, false);
    delete win.umami;
    assert.equal(reportReleaseCheck(win, video(win, { 'data-resource-id': HASH }), 'hevc'), false);
    assert.notEqual(win.sessionStorage.getItem(RELEASE_KEY), null);
});
