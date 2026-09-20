import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import { seedInitialEntry } from './historySeed.js';

function page(html, url = 'https://webtor.io/ru/?x=1#action=stream') {
    const dom = new JSDOM(`<!doctype html><body>${html}</body>`, { url });
    return dom.window;
}

test('the entry the visit started on gets the state Back needs', () => {
    const w = page('<main id="main" data-async-layout="{{ template &quot;main&quot; . }}"></main>');
    assert.equal(w.history.state, null, 'fixture: a server-rendered page has none');
    assert.equal(seedInitialEntry(w, w.document), true);
    const s = w.history.state;
    // Exactly the fields the popstate handler in async.js demands.
    assert.equal(s.context, 'links');
    assert.equal(s.targetSelector, 'main');
    assert.equal(s.url, '/ru/?x=1', 'no hash: it is not part of what the server renders');
    assert.ok(s.layout);
    assert.equal(w.location.href, 'https://webtor.io/ru/?x=1#action=stream', 'the address itself is untouched');
});

test('an entry somebody already described is left alone', () => {
    const w = page('<main data-async-layout="x"></main>');
    w.history.replaceState({ mine: true }, '');
    assert.equal(seedInitialEntry(w, w.document), false);
    assert.deepEqual(w.history.state, { mine: true });
});

test('a page with nothing to restore into is not seeded', () => {
    const w = page('<div id="embed"></div>');
    assert.equal(seedInitialEntry(w, w.document), false);
    assert.equal(w.history.state, null);
});

test('a history that refuses writes does not break the page', () => {
    const w = page('<main data-async-layout="x"></main>');
    const fake = { history: { state: null, replaceState() { throw new Error('SecurityError'); } }, location: w.location };
    assert.equal(seedInitialEntry(fake, w.document), false);
});
