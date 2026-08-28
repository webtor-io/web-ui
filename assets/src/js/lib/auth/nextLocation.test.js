import test from 'node:test';
import assert from 'node:assert/strict';
import {nextLocation} from './nextLocation.js';

// A successful refresh means the server can now resolve the session, so the
// interstitial reloads the page the visitor was on.
test('successful refresh reloads the current url', () => {
    const loc = {href: 'http://localhost:8083/ru/notifications', pathname: '/ru/notifications', search: ''};
    assert.equal(nextLocation(true, loc), 'http://localhost:8083/ru/notifications');
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
