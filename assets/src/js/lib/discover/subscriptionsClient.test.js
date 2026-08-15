import test from 'node:test';
import assert from 'node:assert/strict';

// The module reaches for three browser things at import time or on first
// call: the locale list webpack injects, the <html lang> the language prefix
// is read from, and window._CSRF. Stubbing them here is what lets the wire
// format be tested without a browser.
globalThis.__SUPPORTED_LOCALES__ = ['en', 'ru'];
globalThis.document = { documentElement: { lang: 'en' } };
globalThis.window = { _CSRF: 'csrf-token' };

const { subscriptionKey, itemKey, fetchSubscriptionIds, subscribe, unsubscribe } =
    await import('./subscriptionsClient.js');

// stubFetch records the calls and answers with a canned response.
function stubFetch(response) {
    const calls = [];
    globalThis.fetch = async (url, opts = {}) => {
        calls.push({ url, ...opts });
        if (response instanceof Error) throw response;
        return {
            ok: response.status ? response.status < 400 : true,
            status: response.status || 200,
            json: async () => response.body,
        };
    };
    return calls;
}

test('a season key carries the season, a movie key does not', () => {
    assert.equal(subscriptionKey('season', 'tt1190634', 3), 'season:tt1190634:3');
    assert.equal(subscriptionKey('movie', 'tt0111161'), 'movie:tt0111161');
});

// The whole point of the key: two seasons of one series are two different
// subscriptions, and must not collapse into one bell.
test('seasons of the same series get different keys', () => {
    assert.notEqual(subscriptionKey('season', 'tt1', 2), subscriptionKey('season', 'tt1', 3));
});

test('a server item and a local target produce the same key', () => {
    const fromServer = itemKey({ kind: 'season', video_id: 'tt1190634', season: 3 });
    assert.equal(fromServer, subscriptionKey('season', 'tt1190634', 3));
    assert.equal(itemKey({ kind: 'movie', video_id: 'tt0111161' }), subscriptionKey('movie', 'tt0111161'));
});

test('fetchSubscriptionIds maps the server list into keys', async () => {
    stubFetch({
        body: {
            items: [
                { kind: 'season', video_id: 'tt1190634', season: 3 },
                { kind: 'movie', video_id: 'tt0111161' },
            ],
            count: 2,
            limit: 3,
        },
    });
    const { keys } = await fetchSubscriptionIds();
    assert.deepEqual(keys, ['season:tt1190634:3', 'movie:tt0111161']);
});

// A failed prefetch must not take the page down with it: an unfilled bell is
// a smaller wrong than a modal that refuses to render.
test('fetchSubscriptionIds degrades to an empty set', async () => {
    stubFetch({ status: 500, body: {} });
    assert.deepEqual(await fetchSubscriptionIds(), { keys: [] });

    stubFetch(new Error('offline'));
    assert.deepEqual(await fetchSubscriptionIds(), { keys: [] });
});

test('subscribe posts the content key and the CSRF token', async () => {
    const calls = stubFetch({ body: { status: 'success', added: true, message: 'Subscribed' } });
    const res = await subscribe({ kind: 'season', videoId: 'tt1190634', season: 3, source: 'discover_season' });

    assert.equal(res.ok, true);
    assert.equal(res.added, true);
    assert.equal(res.message, 'Subscribed');

    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, '/discover/subscriptions');
    assert.equal(calls[0].method, 'POST');
    assert.equal(calls[0].headers['X-CSRF-TOKEN'], 'csrf-token');
    assert.deepEqual(JSON.parse(calls[0].body), {
        kind: 'season',
        video_id: 'tt1190634',
        season: 3,
        source: 'discover_season',
    });
});

test('a movie subscribe sends no season', async () => {
    const calls = stubFetch({ body: { added: true } });
    await subscribe({ kind: 'movie', videoId: 'tt0111161', season: 7, source: 'empty_streams' });
    assert.equal(JSON.parse(calls[0].body).season, 0);
});

// The three refusals the UI has to tell apart: the cap, a season that has
// finished airing, and everything else. Each one keeps the server's own
// message, because that is what is in the reader's language.
test('subscribe surfaces the refusal code and the server message', async () => {
    stubFetch({ status: 402, body: { code: 'limit_exceeded', message: 'Limit reached', limit: 3 } });
    const limited = await subscribe({ kind: 'movie', videoId: 'tt1' });
    assert.deepEqual(limited, { ok: false, code: 'limit_exceeded', message: 'Limit reached', limit: 3 });

    stubFetch({ status: 409, body: { code: 'not_eligible', message: 'Season is over' } });
    const over = await subscribe({ kind: 'season', videoId: 'tt1', season: 1 });
    assert.equal(over.code, 'not_eligible');
    assert.equal(over.message, 'Season is over');

    stubFetch({ status: 500, body: {} });
    const broken = await subscribe({ kind: 'movie', videoId: 'tt1' });
    assert.equal(broken.code, 'http_500');
});

test('a network failure is a value, not a throw', async () => {
    stubFetch(new Error('offline'));
    assert.deepEqual(await subscribe({ kind: 'movie', videoId: 'tt1' }), { ok: false, code: 'network', message: '' });
    stubFetch(new Error('offline'));
    assert.deepEqual(await unsubscribe({ kind: 'movie', videoId: 'tt1' }), { ok: false, code: 'network', message: '' });
});

test('unsubscribe addresses a season by query and a movie without one', async () => {
    let calls = stubFetch({ body: { removed: true, message: 'Unsubscribed' } });
    const res = await unsubscribe({ kind: 'season', videoId: 'tt1190634', season: 3 });
    assert.equal(res.ok, true);
    assert.equal(calls[0].method, 'DELETE');
    assert.equal(calls[0].url, '/discover/subscriptions/season/tt1190634?season=3');

    calls = stubFetch({ body: {} });
    await unsubscribe({ kind: 'movie', videoId: 'tt0111161' });
    assert.equal(calls[0].url, '/discover/subscriptions/movie/tt0111161');
});

// Ids ride in a path segment, so anything exotic in one has to be escaped
// rather than reinterpreted as more path.
test('video ids are escaped into the path', async () => {
    const calls = stubFetch({ body: {} });
    await unsubscribe({ kind: 'movie', videoId: 'tt1/../admin' });
    assert.equal(calls[0].url, '/discover/subscriptions/movie/tt1%2F..%2Fadmin');
});

// Non-English pages live under a language prefix; every request has to keep
// it or the server answers a redirect instead of JSON.
test('requests carry the language prefix', async () => {
    globalThis.document.documentElement.lang = 'ru';
    const calls = stubFetch({ body: { items: [] } });
    await fetchSubscriptionIds();
    assert.equal(calls[0].url, '/ru/discover/subscriptions/ids');
    globalThis.document.documentElement.lang = 'en';
});
