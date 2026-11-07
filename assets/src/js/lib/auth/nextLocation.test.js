import test from 'node:test';
import assert from 'node:assert/strict';
import {nextLocation, settleRefresh, resumeRefresh} from './nextLocation.js';

// A successful refresh means the server can now resolve the session, so the
// interstitial must re-request the page the visitor was on. That has to be a
// reload (null), not a location.replace(href): when href carries a fragment
// (e.g. /profile#subscriptions from a subscription email), navigating to the
// current URL is a same-document fragment navigation per the HTML spec — no
// server request, and the visitor stays on the blank interstitial forever.
test('successful refresh signals an in-place reload', () => {
    const loc = {href: 'http://localhost:8083/ru/notifications', pathname: '/ru/notifications', search: ''};
    assert.equal(nextLocation(true, loc), null);
});

test('successful refresh signals a reload when the url has a fragment', () => {
    const loc = {href: 'http://localhost:8083/profile#subscriptions', pathname: '/profile', search: ''};
    assert.equal(nextLocation(true, loc), null);
});

// attemptRefreshingSession() resolves false — without throwing and without a
// network call — when the SDK's local session state says no session exists,
// even though the server still sees an (expired) access token cookie and
// keeps rendering this interstitial. Reloading in that state loops forever;
// the only way forward is a fresh login, back to where the visitor was going.
test('failed refresh goes to login with the original path as return-url', () => {
    const loc = {href: 'http://localhost:8083/ru/notifications', pathname: '/ru/notifications', search: ''};
    assert.equal(nextLocation(false, loc), '/login?return-url=%2Fru%2Fnotifications&from=session-ended');
});

test('failed refresh keeps the query string in return-url', () => {
    const loc = {href: 'http://localhost:8083/library?sort=name', pathname: '/library', search: '?sort=name'};
    assert.equal(nextLocation(false, loc), '/login?return-url=%2Flibrary%3Fsort%3Dname&from=session-ended');
});

// attemptRefreshingSession() rejects instead of resolving false when the
// refresh request failed while the SDK still holds the session: any answer
// >= 300 but 401 (500 from a SuperTokens core or Postgres that is down, as on
// 2026-10-05) or no answer at all. That says nothing about whether the session
// exists; sending the visitor to the login form for it took signed-in users
// to /login for the length of an outage.
test('a refresh the server fails is not "no session"', async () => {
    const loc = {pathname: '/library', search: ''};
    const failed = new Response('', {status: 500});
    const res = await settleRefresh(() => Promise.reject(failed), loc);
    assert.equal(res.error, failed);
    assert.equal(res.next, undefined);
});

test('a refresh with no answer at all is not "no session"', async () => {
    const loc = {pathname: '/library', search: ''};
    const res = await settleRefresh(() => Promise.reject(new TypeError('Failed to fetch')), loc);
    assert.ok(res.error instanceof TypeError);
    assert.equal(res.next, undefined);
});

test('a refresh that finds no session settles on the login form', async () => {
    const loc = {pathname: '/library', search: '?sort=name'};
    assert.deepEqual(await settleRefresh(async () => false, loc), {next: '/login?return-url=%2Flibrary%3Fsort%3Dname&from=session-ended'});
});

test('a refreshed session settles on a reload', async () => {
    const loc = {pathname: '/library', search: ''};
    assert.deepEqual(await settleRefresh(async () => true, loc), {next: null});
});

// The visitor asked for the page, fragment included: a magic link or
// /profile#subscriptions out of an email must come back whole after login.
test('failed refresh keeps the fragment in return-url', () => {
    const loc = {pathname: '/profile', search: '', hash: '#subscriptions'};
    assert.equal(nextLocation(false, loc), '/login?return-url=%2Fprofile%23subscriptions&from=session-ended');
});

function memStorage() {
    const m = new Map();
    return {getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, String(v))};
}
const broken = {getItem() { throw new Error('denied'); }, setItem() { throw new Error('denied'); }};

// A fake window.location plus failed(), recording what the interstitial did.
function env(storage, now) {
    const did = [];
    return {
        did,
        env: {
            location: {pathname: '/library', search: '', hash: '', reload: () => did.push('reload'), replace: (u) => did.push('replace ' + u)},
            storage, now,
            failed: () => did.push('failed'),
        },
    };
}

test('a refreshed session reloads into the page', async () => {
    const {did, env: e} = env(memStorage(), 1_000_000);
    await resumeRefresh(async () => true, e);
    assert.deepEqual(did, ['reload']);
});

// Two successful refreshes in one tab within a minute: the reload came back
// to the interstitial, so the new cookies were not kept. Reloading again
// would loop forever.
test('a second refresh within a minute stops instead of looping', async () => {
    const storage = memStorage();
    const first = env(storage, 1_000_000);
    await resumeRefresh(async () => true, first.env);
    const second = env(storage, 1_020_000);
    await resumeRefresh(async () => true, second.env);
    assert.deepEqual(second.did, ['failed']);
    const later = env(storage, 1_061_000);
    await resumeRefresh(async () => true, later.env);
    assert.deepEqual(later.did, ['reload']);
});

// Reloading is the only way to show a refreshed page, so storage that throws
// (Safari private mode, blocked site data) must not strand the visitor.
test('a refreshed session reloads even without storage', async () => {
    const {did, env: e} = env(broken, 1_000_000);
    await resumeRefresh(async () => true, e);
    assert.deepEqual(did, ['reload']);
});

test('a refresh that finds no session goes to the login form', async () => {
    const {did, env: e} = env(memStorage(), 1_000_000);
    await resumeRefresh(async () => false, e);
    assert.deepEqual(did, ['replace /login?return-url=%2Flibrary&from=session-ended']);
});

// No answer (outage, network, CSRF mismatch after the gin session expired):
// one reload brings a fresh token and outlasts a blip; a second failure says
// so instead of looping, and never shows the login form.
test('a refresh that gets no answer reloads once, then says so', async () => {
    const storage = memStorage();
    const fail = () => Promise.reject(new Response('', {status: 500}));
    const first = env(storage, 1_000_000);
    await resumeRefresh(fail, first.env);
    assert.deepEqual(first.did, ['reload']);
    const second = env(storage, 1_010_000);
    await resumeRefresh(fail, second.env);
    assert.deepEqual(second.did, ['failed']);
});

// The retry and the post-refresh reload are separate budgets: a reload after
// a failed attempt must not use up the one that shows the refreshed page.
test('a retry does not use up the reload after a successful refresh', async () => {
    const storage = memStorage();
    await resumeRefresh(() => Promise.reject(new TypeError('Failed to fetch')), env(storage, 1_000_000).env);
    const ok = env(storage, 1_005_000);
    await resumeRefresh(async () => true, ok.env);
    assert.deepEqual(ok.did, ['reload']);
});

test('without storage a failed refresh says so rather than risk a loop', async () => {
    const {did, env: e} = env(broken, 1_000_000);
    await resumeRefresh(() => Promise.reject(new TypeError('Failed to fetch')), e);
    assert.deepEqual(did, ['failed']);
});
