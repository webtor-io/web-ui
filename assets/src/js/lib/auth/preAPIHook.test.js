import test from 'node:test';
import assert from 'node:assert/strict';
import {preAPIHook} from './preAPIHook.js';

// Session.init gets the hook once per page, with the token of that moment. A
// tab that took a fresh one since (useWatchHistory, from a CSRF 400) must send
// the fresh one on the refresh, or the refresh fails CSRF as well.
test('the hook sends the page\'s current token, not the one it was made with', (t) => {
    t.after(() => { delete globalThis.window; });
    globalThis.window = {_CSRF: 'page-token'};
    const hook = preAPIHook('page-token');
    window._CSRF = 'fresh-token';
    const {requestInit, url} = hook({url: '/auth/session/refresh', requestInit: {method: 'post', headers: {'st-auth-mode': 'cookie'}}});
    assert.equal(url, '/auth/session/refresh');
    assert.equal(requestInit.method, 'post');
    assert.deepEqual(requestInit.headers, {'st-auth-mode': 'cookie', 'X-CSRF-TOKEN': 'fresh-token'});
});

test('without a page token the hook falls back to the one it was made with', (t) => {
    t.after(() => { delete globalThis.window; });
    globalThis.window = {};
    const {requestInit} = preAPIHook('init-token')({url: '/x', requestInit: {headers: {}}});
    assert.equal(requestInit.headers['X-CSRF-TOKEN'], 'init-token');
});
