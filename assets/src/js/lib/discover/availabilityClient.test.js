import test from 'node:test';
import assert from 'node:assert/strict';

// Browser things the imports reach for (see subscriptionsClient.test.js).
globalThis.__SUPPORTED_LOCALES__ = ['en', 'ru'];
globalThis.document = { documentElement: { lang: 'en' } };
globalThis.window = { _CSRF: 'csrf-token' };
const { applyCached, availabilityItems, withCached } = await import('./availabilityClient.js');

const H = n => String(n).repeat(40).slice(0, 40);
const s = (n, extra = {}) => ({ name: `src${n}`, infoHash: H(n), ...extra });

test('cached streams go first, both groups keep their order', () => {
    const out = applyCached([s(1), s(2), s(3), s(4), s(5)], [3, 1]);
    assert.deepEqual(out.map(x => x.name), ['src2', 'src4', 'src1', 'src3', 'src5']);
    assert.deepEqual(out.map(x => !!x.cached), [true, true, false, false, false]);
});

test('nothing cached: the very same list, untouched', () => {
    const list = [s(1), s(2)];
    assert.equal(applyCached(list, []), list);
    assert.equal(applyCached(list, undefined), list);
    // positions that are not positions of this list are not trusted
    assert.equal(applyCached(list, [7, -1, 'x', 1.5]), list);
});

test('the input is not mutated', () => {
    const list = [s(1), s(2)];
    applyCached(list, [1]);
    assert.equal(list[1].cached, undefined);
});

test('request items keep positions, including streams without a hash', () => {
    const items = availabilityItems([
        s(1, { fileIdx: 0 }),
        { name: 'no hash', externalUrl: 'https://example.com/' },
        s(3, { fileIdx: '4' }),
        s(4),
        { url: `magnet:?xt=urn:btih:${H(5).toUpperCase()}` },
    ]);
    assert.deepEqual(items, [
        { infoHash: H(1), fileIdx: 0 },
        { infoHash: '' },
        { infoHash: H(3), fileIdx: 4 },
        { infoHash: H(4) },
        { infoHash: H(5) },
    ]);
});

test('a failing, slow or odd server leaves the list as it was', async () => {
    const list = [s(1), s(2)];
    const cases = [
        async () => { throw new Error('network'); },
        async () => ({ ok: false, status: 500, json: async () => ({}) }),
        async () => ({ ok: true, json: async () => { throw new Error('not json'); } }),
        async () => ({ ok: true, json: async () => null }),
    ];
    for (const fetchImpl of cases) {
        assert.equal(await withCached(list, { fetchImpl }), list);
    }
});

test('a good answer is applied; a list with no hashes is not asked about', async () => {
    let calls = 0;
    const fetchImpl = async (url, opts) => {
        calls++;
        assert.match(url, /\/discover\/availability$/);
        assert.equal(JSON.parse(opts.body).items.length, 2);
        // The route is a POST on the main engine, so the CSRF middleware
        // answers 400 without this header -- and withCached would swallow that
        // as "no answer": the chip would silently never appear.
        assert.equal(opts.method, 'POST');
        assert.equal(opts.headers['X-CSRF-TOKEN'], 'csrf-token');
        assert.equal(opts.headers['Content-Type'], 'application/json');
        return { ok: true, json: async () => ({ cached: [1] }) };
    };
    const out = await withCached([s(1), s(2)], { fetchImpl });
    assert.deepEqual(out.map(x => x.name), ['src2', 'src1']);
    await withCached([{ name: 'x' }], { fetchImpl });
    assert.equal(calls, 1);
});
