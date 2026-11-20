// Deleting a file or a folder of the owner's upload from its page: a token
// from web-ui, then a DELETE straight to the seeder.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM(`<!doctype html><html><body>
<button type="button" id="folder" data-upload-delete="/Name/dir/" data-confirm="Delete the folder?" data-upload-id="U" data-upload-token="/upload/token" data-error="e" data-busy="b">x</button>
<button type="button" id="file" data-upload-delete="/Name/a.mkv" data-upload-id="U" data-upload-token="/upload/token" data-error="e" data-busy="b">x</button>
</body></html>`, { url: 'https://webtor.io/U' });
global.window = dom.window;
global.document = dom.window.document;
window._CSRF = 'csrf';
window._sessionID = 'session';

const { deleteUploadFile, bindUploadFileDelete } = await import('./uploadFile.js');

const grant = { url: 'https://up.example.invalid/U', token: 't', apiKey: 'k' };

// fetchStub records its calls; the token request answers tokenStatus, the
// DELETE deleteStatus.
function fetchStub(tokenStatus = 200, deleteStatus = 204) {
    const calls = [];
    const f = async (url, opts) => {
        calls.push([url, opts]);
        if (opts.method === 'POST') {
            return { ok: tokenStatus === 200, status: tokenStatus, json: async () => grant };
        }
        return { ok: deleteStatus < 300, status: deleteStatus, text: async () => 'deletions' };
    };
    f.calls = calls;
    return f;
}

test('a file: a token for one byte, then a DELETE of its path', async () => {
    const f = fetchStub();
    assert.equal(await deleteUploadFile('/upload/token', 'U', '/Name/a b.mkv', f), 204);
    assert.equal(f.calls.length, 2);
    const [url, opts] = f.calls[0];
    assert.equal(url, '/upload/token');
    assert.equal(opts.method, 'POST');
    assert.deepEqual(JSON.parse(opts.body), { id: 'U', want: 1 });
    assert.equal(opts.headers['X-CSRF-TOKEN'], 'csrf');
    assert.equal(opts.headers['X-SESSION-ID'], 'session');
    assert.deepEqual(f.calls[1], ['https://up.example.invalid/U/Name/a%20b.mkv?token=t&api-key=k', { method: 'DELETE' }]);
});

test('a folder keeps its trailing slash', async () => {
    const f = fetchStub();
    await deleteUploadFile('/upload/token', 'U', '/Name/dir/', f);
    assert.deepEqual(JSON.parse(f.calls[0][1].body), { id: 'U', want: 1 });
    assert.equal(f.calls[1][0], 'https://up.example.invalid/U/Name/dir/?token=t&api-key=k');
});

test('no token, no DELETE', async () => {
    const f = fetchStub(401);
    assert.equal(await deleteUploadFile('/upload/token', 'U', '/Name/a.mkv', f), 401);
    assert.equal(f.calls.length, 1);
});

test('the buttons', async () => {
    let answer = true;
    const asked = [];
    const toasts = [];
    let reloads = 0;
    let f = fetchStub();
    global.confirm = (msg) => { asked.push(msg); return answer; };
    global.fetch = (...args) => f(...args);
    global.location = { reload: () => { reloads++; } };
    window.toast = { error: (msg) => toasts.push(msg) };
    bindUploadFileDelete(document);
    const click = async (id) => {
        document.getElementById(id).click();
        await new Promise((r) => setTimeout(r, 0));
    };

    window.__uploadBusy = true;
    await click('folder');
    await click('file');
    assert.deepEqual(asked, []);
    assert.equal(f.calls.length, 0);
    assert.equal(reloads, 0);
    assert.deepEqual(toasts, ['b', 'b']);

    window.__uploadBusy = false;
    answer = false;
    await click('folder');
    assert.deepEqual(asked, ['Delete the folder?']);
    assert.equal(f.calls.length, 0);
    assert.equal(reloads, 0);

    answer = true;
    await click('folder');
    assert.equal(f.calls.length, 2);
    assert.equal(f.calls[1][0], 'https://up.example.invalid/U/Name/dir/?token=t&api-key=k');
    assert.equal(f.calls[1][1].method, 'DELETE');
    assert.equal(reloads, 1);

    await click('file');
    assert.equal(asked.length, 2);
    assert.equal(f.calls.length, 4);
    assert.equal(reloads, 2);

    // The seeder refuses: the deleted list would not fit (413 deletions).
    f = fetchStub(200, 413);
    await click('file');
    assert.equal(f.calls.length, 2);
    assert.equal(reloads, 2);
    assert.deepEqual(toasts, ['b', 'b', 'e']);
});
