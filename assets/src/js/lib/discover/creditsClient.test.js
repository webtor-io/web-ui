import test from 'node:test';
import assert from 'node:assert/strict';

// Browser things the imports reach for (see subscriptionsClient.test.js).
globalThis.__SUPPORTED_LOCALES__ = ['en', 'ru'];
globalThis.document = { documentElement: { lang: 'en' } };
globalThis.window = { _CSRF: 'csrf-token' };
const { fetchCredits, peekCredits } = await import('./creditsClient.js');

let calls = 0;
function respond(status, body) {
    globalThis.fetch = async () => { calls++; return { ok: status === 200, json: async () => body }; };
}

test('an answer is cached and readable synchronously', async () => {
    calls = 0;
    respond(200, { people: [{ name: 'Actor A' }] });
    assert.equal(peekCredits('tt1'), null);
    assert.deepEqual(await fetchCredits('tt1', 'movie'), { people: [{ name: 'Actor A' }] });
    assert.deepEqual(peekCredits('tt1'), { people: [{ name: 'Actor A' }] });
    await fetchCredits('tt1', 'movie');
    assert.equal(calls, 1);
});

test('a failure is not cached, the next open retries', async () => {
    calls = 0;
    respond(502, { error: 'credits lookup failed' });
    assert.equal(await fetchCredits('tt2', 'movie'), null);
    assert.equal(peekCredits('tt2'), null);
    respond(200, {});
    assert.deepEqual(await fetchCredits('tt2', 'movie'), {});
    assert.equal(calls, 2);
});

test('a network error is not cached either', async () => {
    calls = 0;
    globalThis.fetch = async () => { calls++; throw new Error('offline'); };
    assert.equal(await fetchCredits('tt4', 'movie'), null);
    assert.equal(peekCredits('tt4'), null);
    respond(200, {});
    assert.deepEqual(await fetchCredits('tt4', 'movie'), {});
    assert.equal(calls, 2);
});

test('ids the server cannot resolve are not asked for', async () => {
    calls = 0;
    assert.equal(await fetchCredits('tt3:1:2', 'series'), null);
    assert.equal(await fetchCredits('tmdb5', 'movie'), null);
    assert.equal(calls, 0);
});
