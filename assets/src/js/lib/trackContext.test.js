import test from 'node:test';
import assert from 'node:assert/strict';
import {computeFirstTouch, firstTouch} from './trackContext.js';

const now = new Date('2026-09-24T10:00:00Z');
const ft = (href, referrer) => computeFirstTouch(href, referrer, now);

test('the source is utm_source, else the referring host, else direct', () => {
    assert.deepEqual(ft('https://webtor.io/?utm_source=chrome-ext&utm_medium=popup', 'https://www.google.com/'),
        {ft_source: 'chrome-ext', ft_medium: 'popup', ft_path: '/', ft_day: '2026-09-24'});
    assert.equal(ft('https://webtor.io/torrent-player', 'https://www.google.com/').ft_source, 'google.com');
    assert.equal(ft('https://webtor.io/ru/', 'https://yandex.ru/search/?text=x').ft_source, 'yandex.ru');
    assert.equal(ft('https://webtor.io/', 'https://chatgpt.com/').ft_source, 'chatgpt.com');
    assert.equal(ft('https://webtor.io/', '').ft_source, 'direct');
    assert.equal(ft('https://webtor.io/', 'not a url').ft_source, 'direct');
});

// A Cloudflare challenge reloads the page it stood in front of, so the page
// becomes its own referrer: that is "self", not a channel.
test('webtor.io as the referrer is self', () => {
    for (const ref of ['https://webtor.io/abc', 'https://blog.webtor.io/', 'https://WEBTOR.IO./']) {
        assert.equal(ft('https://webtor.io/', ref).ft_source, 'self', ref);
    }
    assert.equal(ft('https://webtor.io/', 'https://webtor.io.evil.example/').ft_source, 'webtor.io.evil.example');
});

test('the landing path hides info hashes and every value is bounded', () => {
    assert.equal(ft('https://webtor.io/ru/08ada5a7a6183aae1e09d831df6748d566095a10?file=a', '').ft_path, '/ru/:hash');
    const long = ft('https://webtor.io/?utm_source=' + 'x'.repeat(200), '');
    assert.equal(long.ft_source.length, 64);
    assert.equal(ft('::', ''), null);
});

function fakeWindow(href, referrer, store = new Map()) {
    return {
        location: {href},
        document: {referrer},
        localStorage: {
            getItem: k => (store.has(k) ? store.get(k) : null),
            setItem: (k, v) => store.set(k, String(v)),
        },
        store,
    };
}

// The first touch is recorded once: a later visit from elsewhere does not
// overwrite where the browser first came from.
test('the first touch is kept across visits', () => {
    const first = fakeWindow('https://webtor.io/torrent-player', 'https://www.google.com/');
    assert.equal(firstTouch(first, now).ft_source, 'google.com');
    const later = fakeWindow('https://webtor.io/?utm_source=newsletter', '', first.store);
    assert.equal(firstTouch(later, new Date('2026-10-01T00:00:00Z')).ft_source, 'google.com');
    assert.equal(firstTouch(later).ft_day, '2026-09-24');
});

test('broken or missing storage never breaks the page', () => {
    const w = fakeWindow('https://webtor.io/', '');
    w.localStorage.getItem = () => { throw new Error('blocked'); };
    assert.deepEqual(firstTouch(w, now), {});
    const junk = fakeWindow('https://webtor.io/', 'https://www.bing.com/', new Map([['webtor.first_touch', '{not json']]));
    assert.equal(firstTouch(junk, now).ft_source, 'bing.com');
    const empty = fakeWindow('https://webtor.io/', 'https://www.bing.com/', new Map([['webtor.first_touch', '{}']]));
    assert.equal(firstTouch(empty, now).ft_source, 'bing.com');
});
