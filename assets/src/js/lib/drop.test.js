// The home page's drop zone and an upload's page: a .torrent opens as ever,
// own files and folders go to the uploader, a guest gets the sign-in card,
// and nothing is lost or doubled while an upload runs.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const U0 = '9c5db5752b7c0123456789abcdef0123456789ab';
const U1 = '9c5db5752b7c1123456789abcdef0123456789ab';
const T = '0123456789abcdef0123456789abcdef01234567';
const BUSY = 'Wait until the upload finishes';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://example.com/' });
global.window = dom.window;
global.document = dom.window.document;

const { collect, torrentsOnly, initDrop, initAddFiles, markBusy, clearBusy, busyMarked } = await import('./drop.js');

const tick = () => new Promise((r) => setTimeout(r, 0));

// ownWindow puts a fresh window in place for one test: listeners other tests
// left on the shared document would cancel or take its events.
function ownWindow(t, url) {
    const prev = [global.window, global.document];
    const w = (url ? new JSDOM('<!doctype html><body></body>', { url }) : new JSDOM('<!doctype html><body></body>')).window;
    global.window = w;
    global.document = w.document;
    t.after(() => { [global.window, global.document] = prev; });
    return w;
}

const file = (name, size = 10) => ({ name, size });
const fileEntry = (name) => ({ isFile: true, isDirectory: false, name, fullPath: '/' + name });
const dirEntry = (name) => ({ isFile: false, isDirectory: true, name, fullPath: '/' + name });

// dataTransfer is what a drop of these {entry, file} pairs carries; dead
// empties its items, as the browser does once the event is over.
function dataTransfer(pairs, types = ['Files']) {
    const dt = {
        dead: false,
        types,
        files: pairs.map((p) => p.file),
        get items() {
            return dt.dead ? [] : pairs.map((p) => ({ kind: 'file', webkitGetAsEntry: () => p.entry, getAsFile: () => p.file }));
        },
    };
    return dt;
}

const dropped = (...names) => names.map((n) => (n.endsWith('/')
    ? { entry: dirEntry(n.slice(0, -1)), file: file(n.slice(0, -1), 0) }
    : { entry: fileEntry(n), file: file(n) }));

function dropOn(target, dt) {
    const e = new target.ownerDocument.defaultView.Event('drop', { bubbles: true, cancelable: true });
    e.dataTransfer = dt;
    target.dispatchEvent(e);
    return e;
}

// withFiles gives an input a settable file list: jsdom takes only its own
// FileList, which a test cannot build.
function withFiles(input, files = []) {
    let list = files;
    Object.defineProperty(input, 'files', { configurable: true, get: () => list, set: (v) => { list = v; } });
    return (next) => { list = next; input.dispatchEvent(new input.ownerDocument.defaultView.Event('change')); };
}

// fakeLoad stands in for the uploader chunk: it counts its loads and records
// what startUpload got.
function fakeLoad() {
    const load = () => {
        load.count++;
        return Promise.resolve({
            startUpload: (host, items, opts) => load.calls.push({ host, items, opts }),
            rootName: () => 'Root',
        });
    };
    load.count = 0;
    load.calls = [];
    return load;
}

// home mounts the drop zone in the shared document and takes it out after
// the test: a form left behind would keep its document listener alive.
function home(t, { guest = false, vault = false } = {}) {
    const root = document.createElement('div');
    root.innerHTML = `${guest ? '<template id="upload-sign-in"><div data-umami-impression="private-refused-shown" data-umami-impression-code="upload" data-umami-impression-reason="sign_in"><script src="/assets/private_refusal.js"></script></div></template>' : ''}
<div data-upload-log></div>
<div class="upload-sign-in-slot"></div>
<form class="dropzone" data-uploads data-upload-busy="${BUSY}"${vault ? ' data-upload-vault' : ''}><div><p>zone</p><button type="button" class="dropzone-folder"><svg><path/></svg>folder</button></div><input class="dropzone-input" type="file" multiple></form>
<input type="file" class="dropzone-dir hidden" webkitdirectory multiple>`;
    document.body.append(root);
    const said = [];
    window.toast = { error: (m) => said.push(m) };
    let net = 0;
    window.fetch = () => { net++; };
    window.XMLHttpRequest = function () { net++; };
    t.after(() => {
        root.remove();
        delete window.toast;
        delete window.__uploadBusy;
    });
    const form = root.querySelector('.dropzone');
    let submits = 0;
    form.requestSubmit = () => { submits++; };
    const dir = root.querySelector('.dropzone-dir');
    let dirClicks = 0;
    dir.click = () => { dirClicks++; };
    let fileClicks = 0;
    form.querySelector('.dropzone-input').click = () => { fileClicks++; };
    return {
        root, form, said, dir,
        slot: root.querySelector('.upload-sign-in-slot'),
        folder: root.querySelector('.dropzone-folder'),
        pick: withFiles(form.querySelector('.dropzone-input')),
        pickDir: withFiles(dir),
        submits: () => submits,
        dirClicks: () => dirClicks,
        fileClicks: () => fileClicks,
        net: () => net,
    };
}

test('torrentsOnly', () => {
    assert.equal(torrentsOnly(dropped('a.torrent')), true);
    assert.equal(torrentsOnly(dropped('a.torrent', 'B.TORRENT')), true);
    assert.equal(torrentsOnly(dropped('a.torrent', 'b.mkv')), false);
    assert.equal(torrentsOnly(dropped('dir/')), false);
    assert.equal(torrentsOnly(dropped('b.mkv')), false);
    assert.equal(torrentsOnly([]), true);
});

test('collect falls back to the files of a drop without items', () => {
    const f = file('a.mkv');
    assert.deepEqual(collect({ files: [f] }), [{ entry: null, file: f }]);
});

test('signed in: .torrent files open as ever, own files go to the uploader', async (t) => {
    for (const [names, submits, loads] of [[['a.torrent'], 1, 0], [['a.torrent', 'b.torrent'], 1, 0], [['a.torrent', 'b.mkv'], 0, 1]]) {
        await t.test(names.join(' + '), async (t) => {
            const h = home(t);
            const load = fakeLoad();
            initDrop(h.form, load);
            dropOn(document.body, dataTransfer(dropped(...names)));
            await tick();
            assert.equal(h.submits(), submits);
            assert.equal(load.count, loads);
        });
    }
});

test('a dropped folder is read while the event lasts', async (t) => {
    const h = home(t);
    const load = fakeLoad();
    initDrop(h.form, load);
    const dt = dataTransfer(dropped('Photos/'));
    dropOn(document.body, dt);
    dt.dead = true;
    await tick();
    assert.equal(load.count, 1);
    assert.equal(load.calls[0].items[0].entry.isDirectory, true);
    assert.equal(load.calls[0].host, h.root.querySelector('[data-upload-log]'));
});

test('a guest: two drops, one event, no script', async (t) => {
    const h = home(t, { guest: true });
    const tracked = [];
    window.umami = { track: (name, data) => tracked.push([name, data]) };
    t.after(() => { delete window.umami; });
    const load = fakeLoad();
    initDrop(h.form, load);
    dropOn(document.body, dataTransfer(dropped('a.mkv')));
    dropOn(document.body, dataTransfer(dropped('b.mkv')));
    await tick();
    assert.equal(h.slot.querySelectorAll('[data-umami-impression]').length, 1);
    assert.equal(h.slot.querySelector('script'), null);
    assert.deepEqual(tracked, [['private-refused-shown', { code: 'upload', reason: 'sign_in' }]]);
    assert.equal(h.net(), 0);
    assert.equal(load.count, 0);
});

test('a guest: Upload a folder shows the card and opens no picker', async (t) => {
    const h = home(t, { guest: true });
    const load = fakeLoad();
    initDrop(h.form, load);
    h.folder.click();
    assert.equal(h.slot.querySelectorAll('[data-umami-impression]').length, 1);
    assert.equal(h.dirClicks(), 0);
    assert.equal(h.fileClicks(), 0);
    assert.equal(load.count, 0);
});

test('Upload a folder sits in the zone and opens the folder picker only', async (t) => {
    const h = home(t);
    initDrop(h.form, fakeLoad());
    assert.ok(h.form.contains(h.folder));
    h.folder.click();
    // A click lands on the icon as often as on the text.
    h.folder.querySelector('path').dispatchEvent(new window.MouseEvent('click', { bubbles: true }));
    assert.equal(h.dirClicks(), 2);
    assert.equal(h.fileClicks(), 0);
    // The rest of the zone still opens the file picker.
    h.form.querySelector('p').click();
    assert.equal(h.fileClicks(), 1);
    assert.equal(h.dirClicks(), 2);
});

test('the markup says whether Vault is set up', async (t) => {
    await t.test('the drop zone', async (t) => {
        const h = home(t);
        const load = fakeLoad();
        initDrop(h.form, load);
        dropOn(document.body, dataTransfer(dropped('b.mkv')));
        await tick();
        assert.equal(load.calls[0].opts.vault, false);
        h.form.setAttribute('data-upload-vault', '');
        dropOn(document.body, dataTransfer(dropped('b.mkv')));
        h.pick([file('b.mkv')]);
        h.pickDir([{ name: 'a', size: 1, webkitRelativePath: 'Photos/a' }]);
        await tick();
        assert.deepEqual(load.calls.slice(1).map((c) => c.opts.vault), [true, true, true]);
    });
    for (const vault of [true, false]) {
        await t.test(`the add button, vault ${vault}`, async (t) => {
            const w = ownWindow(t);
            w.document.body.innerHTML = `<button data-upload-add data-upload-id="${U0}" data-upload-name="Photos"${vault ? ' data-upload-vault' : ''}></button><input type="file" multiple hidden><p id="elsewhere"></p>`;
            const btn = w.document.querySelector('[data-upload-add]');
            const load = fakeLoad();
            initAddFiles(btn, load);
            withFiles(btn.nextElementSibling)([file('a.mkv')]);
            dropOn(w.document.getElementById('elsewhere'), dataTransfer(dropped('a.mkv')));
            await tick();
            assert.deepEqual(load.calls.map((c) => c.opts.vault), [vault, vault]);
        });
    }
});

// addPage puts the add button on its own window, with the listing's card
// when dir is given (null: no card).
function addPage(t, { dir = null, cardID = U0, buttonDir, busy = false } = {}) {
    const w = ownWindow(t);
    w.document.body.innerHTML = `<button data-upload-add data-upload-id="${U0}" data-upload-name="Photos" data-upload-upsell${buttonDir !== undefined ? ` data-upload-dir="${buttonDir}"` : ''}${busy ? ` data-busy="${BUSY}"` : ''}></button><input type="file" multiple hidden>
<div data-upload-log></div>
${dir !== null ? `<div id="list"><div class="js-list-card" data-resource-id="${cardID}" data-dir="${dir}"></div></div>` : ''}<p id="elsewhere"></p>`;
    const btn = w.document.querySelector('[data-upload-add]');
    const input = btn.nextElementSibling;
    let clicks = 0;
    input.click = () => { clicks++; };
    return { w, btn, pick: withFiles(input), clicks: () => clicks };
}

test('add files: the button opens the picker, the files go to this upload', async (t) => {
    const p = addPage(t);
    const load = fakeLoad();
    initAddFiles(p.btn, load);
    p.btn.click();
    assert.equal(p.clicks(), 1);
    p.pick([file('a.mkv')]);
    await tick();
    assert.equal(load.count, 1);
    assert.deepEqual(load.calls[0].opts, { id: U0, name: 'Photos', upsell: true, vault: false, dir: ['Photos'] });
    assert.equal(load.calls[0].host, p.w.document.querySelector('[data-upload-log]'));
});

test('add files into the open folder', async (t) => {
    for (const [name, cfg, want] of [
        ['/Photos/sub', { dir: '/Photos/sub' }, ['Photos', 'sub']],
        ['/Photos', { dir: '/Photos' }, ['Photos']],
        ['another root', { dir: '/Other/x' }, ['Photos']],
        ['empty', { dir: '' }, ['Photos']],
        ['dot-dot', { dir: '/Photos/../x' }, ['Photos']],
        ['dot', { dir: '/Photos/./x' }, ['Photos']],
        ["another resource's card", { dir: '/Photos/sub', cardID: T }, ['Photos']],
        ['no card, the button says', { buttonDir: '/Photos/sub' }, ['Photos', 'sub']],
        ['the card wins over the button', { dir: '/Photos', buttonDir: '/Photos/sub' }, ['Photos']],
    ]) {
        await t.test(name, async (t) => {
            const p = addPage(t, cfg);
            const load = fakeLoad();
            initAddFiles(p.btn, load);
            p.pick([file('a.mkv')]);
            await tick();
            assert.deepEqual(load.calls[0].opts.dir, want);
        });
    }
    await t.test('dir is read at the moment', async (t) => {
        const p = addPage(t, { dir: '/Photos' });
        const load = fakeLoad();
        initAddFiles(p.btn, load);
        p.w.document.getElementById('list').innerHTML = `<div class="js-list-card" data-resource-id="${U0}" data-dir="/Photos/sub"></div>`;
        p.pick([file('a.mkv')]);
        await tick();
        assert.deepEqual(load.calls[0].opts.dir, ['Photos', 'sub']);
    });
    await t.test('a drop on the button', async (t) => {
        const p = addPage(t, { dir: '/Photos/sub' });
        const load = fakeLoad();
        initAddFiles(p.btn, load);
        dropOn(p.btn, dataTransfer(dropped('a.mkv')));
        await tick();
        assert.deepEqual(load.calls[0].opts.dir, ['Photos', 'sub']);
    });
});

test('add files while an upload runs in the tab: a toast, no second upload', async (t) => {
    const p = addPage(t, { busy: true });
    const said = [];
    p.w.toast = { error: (m) => said.push(m) };
    const load = fakeLoad();
    initAddFiles(p.btn, load);
    p.w.__uploadBusy = true;
    p.btn.click();
    assert.equal(p.clicks(), 0);
    assert.deepEqual(said, [BUSY]);
    p.pick([file('a.mkv')]);
    dropOn(p.btn, dataTransfer(dropped('a.mkv')));
    await tick();
    assert.equal(load.count, 0);
    assert.equal(said.length, 3);
    p.w.__uploadBusy = false;
    p.btn.click();
    assert.equal(p.clicks(), 1);
    assert.equal(said.length, 3);
});

// vaultPage is an upload's page with the pledge button and an open pledge
// modal; click answers false when the click was cancelled.
function vaultPage(w) {
    w.document.body.innerHTML = `<div id="pledge-button"><form><button type="submit" id="save"><svg><path id="icon"/></svg></button></form></div>
<div id="pledge-modal"><form action="/en/vault/add" method="post"><button type="submit" id="store"></button></form><form method="dialog"><button id="cancel"></button></form></div>
<button data-upload-add data-upload-id="${U0}" data-busy="${BUSY}"></button><input type="file">`;
    for (const f of w.document.forms) f.addEventListener('submit', (e) => e.preventDefault());
    const said = [];
    w.toast = { error: (m) => said.push(m) };
    const el = (id) => w.document.getElementById(id);
    const click = (node) => node.dispatchEvent(new w.MouseEvent('click', { bubbles: true, cancelable: true }));
    return { said, el, click, add: w.document.querySelector('[data-upload-add]') };
}

test('vault button is held by an upload in another tab', (t) => {
    const w = ownWindow(t, 'https://example.com/');
    const p = vaultPage(w);
    const set = (id, at) => w.localStorage.setItem('upload-busy:' + id, String(at));
    const load = fakeLoad();
    initAddFiles(p.add, load);
    // 1: no mark
    assert.equal(p.click(p.el('save')), true);
    assert.equal(p.said.length, 0);
    // 2: a fresh mark of this upload
    set(U0, Date.now());
    assert.equal(p.click(p.el('save')), false);
    assert.deepEqual(p.said, [BUSY]);
    assert.equal(p.click(p.el('store')), false);
    assert.equal(p.said.length, 2);
    assert.equal(p.click(p.el('cancel')), true);
    assert.equal(p.said.length, 2);
    // 3: another upload's mark
    w.localStorage.removeItem('upload-busy:' + U0);
    set(U1, Date.now());
    assert.equal(p.click(p.el('save')), true);
    // 4: an old mark
    set(U0, Date.now() - 181000);
    assert.equal(p.click(p.el('save')), true);
    set(U0, Date.now() - 179000);
    assert.equal(p.click(p.el('save')), false);
    // 5: a mark from the future
    set(U0, Date.now() + 3600000);
    assert.equal(p.click(p.el('save')), true);
    // 6: garbage
    w.localStorage.setItem('upload-busy:' + U0, 'x');
    assert.equal(p.click(p.el('save')), true);
    // 7: a page without the add button
    set(U0, Date.now());
    p.add.remove();
    assert.equal(p.click(p.el('save')), true);
    // 8
    assert.equal(load.count, 0);
    assert.equal(p.said.length, 3);
});

test('busy mark helpers', async (t) => {
    await t.test('no storage', (t) => {
        const w = ownWindow(t);
        assert.throws(() => w.localStorage);
        assert.equal(busyMarked(U0), false);
        assert.doesNotThrow(() => markBusy(U0));
        assert.doesNotThrow(() => clearBusy(U0));
        const p = vaultPage(w);
        initAddFiles(p.add, fakeLoad());
        assert.equal(p.click(p.el('save')), true);
        w.__uploadBusy = true;
        assert.equal(p.click(p.el('save')), false);
    });
    await t.test('storage', (t) => {
        ownWindow(t, 'https://example.com/');
        markBusy(U0);
        assert.equal(busyMarked(U0), true);
        assert.equal(busyMarked(U1), false);
        clearBusy(U0);
        assert.equal(busyMarked(U0), false);
    });
});

test('the drop zone while an upload runs in the tab', async (t) => {
    const h = home(t);
    h.root.insertAdjacentHTML('afterbegin', '<section data-upload-running></section>');
    const load = fakeLoad();
    initDrop(h.form, load);
    window.__uploadBusy = false;
    dropOn(document.body, dataTransfer(dropped('b.mkv')));
    await tick();
    assert.equal(load.count, 1);
    assert.equal(h.said.length, 0);
    window.__uploadBusy = true;
    dropOn(document.body, dataTransfer(dropped('b.mkv')));
    await tick();
    assert.equal(load.count, 1);
    assert.deepEqual(h.said, [BUSY]);
    // 1
    h.pick([file('b.mkv')]);
    assert.equal(load.count, 1);
    assert.equal(h.said.length, 2);
    // 2
    h.pickDir([{ name: 'a', size: 1, webkitRelativePath: 'Photos/a' }]);
    assert.equal(load.count, 1);
    assert.equal(h.said.length, 3);
    // 3
    h.folder.click();
    assert.equal(h.dirClicks(), 0);
    assert.equal(h.said.length, 4);
    // 4
    dropOn(document.body, dataTransfer(dropped('a.torrent')));
    assert.equal(h.submits(), 1);
    assert.equal(load.count, 1);
    assert.equal(h.said.length, 4);
    // 5
    h.pick([file('a.torrent')]);
    assert.equal(h.submits(), 2);
    assert.equal(h.said.length, 4);
    // 6
    window.__uploadBusy = false;
    h.folder.click();
    assert.equal(h.dirClicks(), 1);
    assert.equal(h.said.length, 4);
    await tick();
    assert.equal(load.count, 1);
});

test('a detached home drop zone takes no drop', async (t) => {
    const h = home(t);
    const load = fakeLoad();
    initDrop(h.form, load);
    const drop = () => dropOn(document.body, dataTransfer(dropped('b.mkv')));
    drop();
    await tick();
    assert.equal(load.count, 1);
    h.form.remove();
    drop();
    await tick();
    assert.equal(load.count, 1);
    assert.equal(h.submits(), 0);
    assert.equal(h.net(), 0);
    // Home, another page, home again: two drop listeners on the document.
    const h2 = home(t);
    const load2 = fakeLoad();
    initDrop(h2.form, load2);
    drop();
    await tick();
    assert.equal(load2.count, 1);
    assert.equal(load.count, 1);
    window.__uploadBusy = true;
    drop();
    await tick();
    assert.equal(load2.count, 1);
    assert.equal(h2.said.length, 1);
});

// pageDrop is an upload's page on its own window, the add button wired.
function pageDrop(t, { busy = false } = {}) {
    const w = ownWindow(t);
    w.document.body.innerHTML = `<button data-upload-add data-upload-id="${U0}" data-upload-name="Photos"${busy ? ` data-busy="${BUSY}"` : ''}></button><input type="file" multiple hidden><p id="elsewhere"></p>`;
    const btn = w.document.querySelector('[data-upload-add]');
    const elsewhere = w.document.getElementById('elsewhere');
    const drop = (target, types) => dropOn(target, dataTransfer(dropped('a.mkv'), types));
    return { w, btn, elsewhere, drop };
}

test('page drop', async (t) => {
    await t.test('anywhere on the page', async (t) => {
        const p = pageDrop(t);
        const load = fakeLoad();
        initAddFiles(p.btn, load);
        const e = p.drop(p.elsewhere);
        await tick();
        assert.equal(load.count, 1);
        assert.deepEqual(load.calls[0].opts, { id: U0, name: 'Photos', upsell: false, vault: false, dir: ['Photos'] });
        assert.equal(e.defaultPrevented, true);
    });
    await t.test('on the button: one upload, not two', async (t) => {
        const p = pageDrop(t);
        const load = fakeLoad();
        initAddFiles(p.btn, load);
        p.drop(p.btn);
        await tick();
        assert.equal(load.count, 1);
    });
    await t.test('taken deeper, as the subtitles label does', async (t) => {
        const p = pageDrop(t);
        const load = fakeLoad();
        initAddFiles(p.btn, load);
        p.elsewhere.addEventListener('drop', (e) => e.preventDefault());
        p.drop(p.elsewhere);
        await tick();
        assert.equal(load.count, 0);
    });
    await t.test('not files', async (t) => {
        const p = pageDrop(t);
        const load = fakeLoad();
        initAddFiles(p.btn, load);
        const e = p.drop(p.elsewhere, ['text/plain']);
        await tick();
        assert.equal(load.count, 0);
        assert.equal(e.defaultPrevented, false);
    });
    await t.test('no add button any more', async (t) => {
        const p = pageDrop(t);
        const load = fakeLoad();
        initAddFiles(p.btn, load);
        p.btn.remove();
        const e = p.drop(p.elsewhere);
        await tick();
        assert.equal(load.count, 0);
        assert.equal(e.defaultPrevented, false);
    });
    await t.test('wired twice', async (t) => {
        const p = pageDrop(t);
        // A view's init runs on every async render: the page's listeners go
        // on once. (A second drop listener would also leave at
        // defaultPrevented, so the count is what tells.)
        const added = [];
        const addListener = p.w.document.addEventListener.bind(p.w.document);
        p.w.document.addEventListener = (type, ...rest) => { added.push(type); return addListener(type, ...rest); };
        const load = fakeLoad();
        initAddFiles(p.btn, load);
        initAddFiles(p.btn, load);
        assert.deepEqual(added.filter((type) => type === 'drop' || type === 'dragover').sort(), ['dragover', 'drop']);
        p.drop(p.elsewhere);
        await tick();
        assert.equal(load.count, 1);
    });
    await t.test('wired again with another loader', async (t) => {
        const p = pageDrop(t);
        const load = fakeLoad();
        const load2 = fakeLoad();
        initAddFiles(p.btn, load);
        initAddFiles(p.btn, load2);
        p.drop(p.elsewhere);
        await tick();
        assert.equal(load2.count, 1);
        assert.equal(load.count, 0);
    });
    await t.test('after the home page', async (t) => {
        const p = pageDrop(t);
        p.btn.nextElementSibling.remove();
        p.btn.remove();
        p.w.document.body.insertAdjacentHTML('afterbegin', '<form class="dropzone" data-uploads><input class="dropzone-input" type="file"></form>');
        const form = p.w.document.querySelector('.dropzone');
        const loadHome = fakeLoad();
        initDrop(form, loadHome);
        form.remove();
        p.w.document.body.insertAdjacentHTML('afterbegin', `<button data-upload-add data-upload-id="${U0}" data-upload-name="Photos"></button><input type="file" multiple hidden>`);
        const btn = p.w.document.querySelector('[data-upload-add]');
        const load = fakeLoad();
        initAddFiles(btn, load);
        p.drop(p.elsewhere);
        await tick();
        assert.equal(load.count, 1);
        assert.equal(loadHome.count, 0);
    });
    await t.test('dragover', async (t) => {
        const p = pageDrop(t);
        initAddFiles(p.btn, fakeLoad());
        const over = () => {
            const e = new p.w.Event('dragover', { bubbles: true, cancelable: true });
            e.dataTransfer = { types: ['Files'] };
            return p.elsewhere.dispatchEvent(e);
        };
        assert.equal(over(), false);
        p.btn.remove();
        assert.equal(over(), true);
    });
    await t.test('while an upload runs in the tab', async (t) => {
        const p = pageDrop(t, { busy: true });
        const said = [];
        p.w.toast = { error: (m) => said.push(m) };
        const load = fakeLoad();
        initAddFiles(p.btn, load);
        p.w.__uploadBusy = true;
        const e = p.drop(p.elsewhere);
        await tick();
        assert.equal(load.count, 0);
        assert.equal(e.defaultPrevented, true);
        assert.deepEqual(said, [BUSY]);
    });
    await t.test('into the open folder', async (t) => {
        const p = pageDrop(t);
        p.w.document.body.insertAdjacentHTML('beforeend', `<div class="js-list-card" data-resource-id="${U0}" data-dir="/Photos/sub"></div>`);
        const load = fakeLoad();
        initAddFiles(p.btn, load);
        p.drop(p.elsewhere);
        await tick();
        assert.deepEqual(load.calls[0].opts.dir, ['Photos', 'sub']);
    });
});
