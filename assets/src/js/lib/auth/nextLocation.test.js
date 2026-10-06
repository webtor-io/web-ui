import test from 'node:test';
import assert from 'node:assert/strict';
import {nextLocation, settleRefresh} from './nextLocation.js';

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
    assert.equal(nextLocation(false, loc), '/login?return-url=%2Fru%2Fnotifications');
});

test('failed refresh keeps the query string in return-url', () => {
    const loc = {href: 'http://localhost:8083/library?sort=name', pathname: '/library', search: '?sort=name'};
    assert.equal(nextLocation(false, loc), '/login?return-url=%2Flibrary%3Fsort%3Dname');
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
    assert.deepEqual(await settleRefresh(async () => false, loc), {next: '/login?return-url=%2Flibrary%3Fsort%3Dname'});
});

test('a refreshed session settles on a reload', async () => {
    const loc = {pathname: '/library', search: ''};
    assert.deepEqual(await settleRefresh(async () => true, loc), {next: null});
});
