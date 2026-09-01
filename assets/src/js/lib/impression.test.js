import { test } from 'node:test';
import assert from 'node:assert/strict';
import { impressionFromDataset, impressionKey, trackImpressions } from './impression.js';

test('dataset attributes become the event name and lower-camel props', () => {
    const { name, props } = impressionFromDataset({
        umamiImpression: 'release-sub-seen',
        umamiImpressionSource: 'resource_banner',
        umamiImpressionSeason: '3',
        asyncLayout: 'ignored',
    });
    assert.equal(name, 'release-sub-seen');
    assert.deepEqual(props, { source: 'resource_banner', season: '3' });
});

test('key is order-independent', () => {
    assert.equal(impressionKey('e', { a: '1', b: '2' }), impressionKey('e', { b: '2', a: '1' }));
});

function el(dataset) {
    return { dataset, matches: () => true, querySelectorAll: () => [] };
}

test('same offer re-rendered is one impression; a different offer is another', () => {
    const calls = [];
    const umami = { track: (n, p) => calls.push([n, p]) };
    const registry = new Set();
    const banner = el({ umamiImpression: 'release-sub-seen', umamiImpressionSource: 'resource_banner' });
    assert.equal(trackImpressions(banner, umami, registry), 1);
    assert.equal(trackImpressions(banner, umami, registry), 0);
    const other = el({ umamiImpression: 'release-sub-seen', umamiImpressionSource: 'empty_streams' });
    assert.equal(trackImpressions(other, umami, registry), 1);
    assert.deepEqual(calls.map(([, p]) => p.source), ['resource_banner', 'empty_streams']);
});

test('no umami, no root, no name: nothing happens', () => {
    assert.equal(trackImpressions(null, { track() { throw new Error('must not track'); } }, new Set()), 0);
    assert.equal(trackImpressions(el({}), undefined, new Set()), 0);
    assert.equal(trackImpressions(el({ umamiImpressionSource: 'x' }), { track() { throw new Error('nameless'); } }, new Set()), 0);
});
