import test from 'node:test';
import assert from 'node:assert/strict';
import {isCsrfMismatch, claimReload, describeAuthError} from './errors.js';

const i18n = {t: (k) => k, tf: (k, ...a) => `${k}:${a.join(',')}`};

function memStorage() {
    const m = new Map();
    return {getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, String(v))};
}

// What the SDK throws: the raw Response. Over HTTP/2 statusText is ''.
function response(status, body) {
    return new Response(body, {status, statusText: ''});
}

test('a 400 with the CSRF middleware body is a mismatch', async () => {
    assert.equal(await isCsrfMismatch(response(400, 'CSRF token mismatch')), true);
});

test('another 400 is not a mismatch', async () => {
    assert.equal(await isCsrfMismatch(response(400, 'bad email')), false);
    assert.equal(await isCsrfMismatch(response(403, 'CSRF token mismatch')), false);
    assert.equal(await isCsrfMismatch(new Error('x')), false);
    assert.equal(await isCsrfMismatch(undefined), false);
});

test('the check leaves the body for whoever reads it next', async () => {
    const r = response(400, 'CSRF token mismatch');
    await isCsrfMismatch(r);
    assert.equal(await r.text(), 'CSRF token mismatch');
});

test('a mismatch reloads the page once', async () => {
    let reloads = 0;
    const env = {storage: memStorage(), now: 1_000_000, reload: () => reloads++, reloadDelayMs: 0};
    const res = await describeAuthError(response(400, 'CSRF token mismatch'), i18n, env);
    assert.deepEqual(res, {message: 'auth.progress.sessionExpiredReloading', reloading: true});
    await new Promise((r) => setTimeout(r, 5));
    assert.equal(reloads, 1);
});

// If the fresh page fails the same way, reloading does not help: no loop.
test('a second mismatch within a minute asks the viewer instead of reloading', async () => {
    let reloads = 0;
    const storage = memStorage();
    const base = {storage, reload: () => reloads++, reloadDelayMs: 0};
    await describeAuthError(response(400, 'CSRF token mismatch'), i18n, {...base, now: 1_000_000});
    const res = await describeAuthError(response(400, 'CSRF token mismatch'), i18n, {...base, now: 1_030_000});
    assert.deepEqual(res, {message: 'auth.progress.sessionExpired', reloading: false});
    await new Promise((r) => setTimeout(r, 5));
    assert.equal(reloads, 1);
    assert.equal(claimReload(storage, 1_061_000), true);
});

test('without storage there is no reload', async () => {
    const broken = {getItem() { throw new Error('denied'); }, setItem() { throw new Error('denied'); }};
    assert.equal(claimReload(broken, 1), false);
    assert.equal(claimReload(undefined, 1), false);
});

test('any other status says the status, not "unknown error"', async () => {
    const res = await describeAuthError(response(502, 'bad gateway'), i18n, {storage: memStorage()});
    assert.deepEqual(res, {message: 'auth.progress.requestFailed:502', reloading: false});
});

test('messages and status texts pass through as before', async () => {
    assert.equal((await describeAuthError(new Error('Network Error'), i18n)).message, 'network error');
    assert.equal((await describeAuthError({status: 500, statusText: 'Internal'}, i18n)).message, 'internal');
    assert.equal((await describeAuthError({}, i18n)).message, 'auth.progress.unknownError');
});
