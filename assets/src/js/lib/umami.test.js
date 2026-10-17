import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import { init } from './umami.js';

// Umami keys the session by payload.id when it is present, else by
// ip+UA+month. Every send — the automatic pageview, track() and identify() —
// must therefore carry the same id, or session data (tier, ft_source…) and
// the events end up in two different sessions.

const SESSION = 'abc123sessioncookie';

function setup({ id, autoTrack = false, reply } = {}) {
    const dom = new JSDOM('<!doctype html><title>t</title><body></body>', { url: 'https://webtor.io/' });
    const window = dom.window;
    Object.defineProperty(window, 'crypto', { value: globalThis.crypto });
    // The tracker uses the bare globals a browser has; jsdom keeps them on window.
    globalThis.MutationObserver = window.MutationObserver;
    const sent = [];
    const headers = [];
    // Umami 3 /api/send: { cache: token, sessionId, visitId }; a bot gets { beep }.
    let n = 0;
    globalThis.fetch = async (url, { body, headers: h }) => {
        sent.push(JSON.parse(body));
        headers.push(h);
        n++;
        return { json: async () => (reply ? reply(n) : { cache: 'tok' + n, sessionId: 's', visitId: 'v' }) };
    };
    const umami = init(window, {
        website_id: 'w',
        host_url: 'https://umami.example',
        auto_track: autoTrack,
        id,
    });
    return { umami, sent, headers };
}

const settle = () => new Promise(r => setTimeout(r, 20));

test('track() carries the same id as identify()', async () => {
    const { umami, sent } = setup({ id: SESSION });
    await umami.track('x');
    await umami.identify(SESSION, { ft_source: 'direct' });
    const [event, identify] = sent;
    assert.equal(event.type, 'event');
    assert.equal(identify.type, 'identify');
    assert.match(identify.payload.id, /^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    assert.equal(event.payload.id, identify.payload.id);
});

test('without an id the payload has no id at all (ip+UA session, as before)', async () => {
    const { umami, sent } = setup();
    await umami.track('x');
    assert.equal(sent.length, 1);
    assert.equal(Object.hasOwn(sent[0].payload, 'id'), false);
});

test('the second send carries x-umami-cache equal to the first response\'s cache', async () => {
    const { umami, sent, headers } = setup({ id: SESSION });
    await umami.track('a');
    await umami.track('b');
    assert.equal(headers[0]['x-umami-cache'], undefined);
    assert.equal(headers[1]['x-umami-cache'], 'tok1');
    // identify with the same id keeps the token (same session, same visit)
    await umami.identify(SESSION);
    assert.equal(headers[2]['x-umami-cache'], 'tok2');
    assert.equal(sent.length, 3);
});

test('a reply without cache (bot) and an identify with another id drop the token', async () => {
    let bot = false;
    const { umami, headers } = setup({ id: SESSION, reply: n => (bot ? { beep: 'boop' } : { cache: 'tok' + n }) });
    await umami.track('a');
    await umami.identify('another-session', { ft_source: 'direct' });
    // the token named the previous session; the new one needs its own row
    assert.equal(headers[1]['x-umami-cache'], undefined);
    bot = true;
    await umami.track('b');
    await umami.track('c');
    assert.equal(headers[2]['x-umami-cache'], 'tok2');
    assert.equal(headers[3]['x-umami-cache'], undefined);
});

test('the first automatic pageview carries the id', async () => {
    const { sent } = setup({ id: SESSION, autoTrack: true });
    await settle();
    assert.equal(sent.length, 1);
    assert.equal(sent[0].type, 'event');
    assert.equal(sent[0].payload.name, undefined);
    assert.equal(typeof sent[0].payload.id, 'string');
});
