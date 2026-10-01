import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';

const dialog = readFileSync(new URL('./__fixtures__/subtitles-dialog.html', import.meta.url), 'utf8');
const boot = new JSDOM('', { url: 'https://webtor.io/' });
global.window = boot.window; global.document = boot.window.document;
const { createNextItemGo, PREPARED_MAX_AGE_MS } = await import('./next-item-go.js');

function select(scope, id, saved = true, type = 'subtitle') {
    for (const chip of scope.querySelectorAll(`.${type}`)) {
        chip.removeAttribute('data-default');
        chip.removeAttribute('data-saved');
    }
    const chip = scope.querySelector(`.${type}[data-id="${id}"]`);
    chip.dataset.default = 'true';
    if (saved) chip.dataset.saved = 'true';
}

function fixture(t) {
    const dom = new JSDOM(`<form action="/stream-video" data-async-target="#log-i1">
        <input name="resource-id" value="res"><input name="item-id" value="i1"></form>
        <div id="log-i1"><div id="host"><div class="wt-player-stage"><video data-item-id="i1"></video></div>${dialog}</div></div>`,
    { url: 'https://webtor.io/ru/res?file=e01.mkv' });
    const w = dom.window;
    Object.assign(global, { window: w, document: w.document, FormData: w.FormData, CustomEvent: w.CustomEvent });
    select(w.document, 'os-os-en', false);
    let stage = w.document.querySelector('.wt-player-stage');
    let currentItem = 'i1';
    const puts = [], mounts = [], destroyed = [], navigated = [], heads = [];
    global.fetch = async (url, opts) => {
        if (opts?.method === 'PUT') puts.push({ url, ...JSON.parse(opts.body) });
        if (opts?.method === 'HEAD') heads.push(url);
        return { ok: true, status: 200 };
    };
    const render = (id = 'os-os-en', saved = false) => {
        const doc = new w.DOMParser().parseFromString(`<div><video class="player" data-item-id="i2"></video>${dialog}</div>`, 'text/html');
        select(doc, id, saved);
        return doc;
    };
    const create = (fetchRender, options = {}) => createNextItemGo({
        next: { itemId: 'i2', path: 'e02.mkv' }, resourceID: 'res', root: stage.parentNode,
        getStage: () => stage, getToken: async () => '', fetchRender,
        destroyPlayer: () => { destroyed.push(currentItem); stage.replaceChildren(); },
        initPlayer: async (host, opts) => { mounts.push({ host, opts, selected: host.querySelector('.subtitle[data-default="true"]')?.dataset.id }); currentItem = 'i2'; },
        navigate: (url) => navigated.push(url), ...options,
    });
    t.after(() => { w.dispatchEvent(new w.CustomEvent('player_ready')); dom.window.close(); });
    return { w, puts, mounts, destroyed, navigated, heads, render, create, navigateElsewhere() {
        w.document.getElementById('log-i1').remove();
        w.document.body.insertAdjacentHTML('beforeend', '<div id="new-host"><div class="wt-player-stage"><video data-item-id="unrelated"></video></div></div>');
        stage = w.document.querySelector('.wt-player-stage'); currentItem = 'unrelated';
        w.history.pushState({}, '', '/ru/other?file=movie.mkv');
    } };
}

test('Next preserves the distinction between automatic subtitles and a saved choice', async (t) => {
    for (const [id, saved] of [['os-os-en', false], ['none', false], ['os-os-en', true], ['none', true]]) {
        await t.test(`${id}, saved=${saved}`, async (t) => {
            const p = fixture(t);
            await p.create(async () => p.render(id, saved)).go('auto');
            assert.equal(p.mounts.length, 1);
            assert.deepEqual(p.puts.filter(p => p.url.endsWith('/subtitle')),
                saved ? [{ url: '/stream-video/subtitle', id, resourceID: 'res', itemID: 'i2' }] : []);
            assert.equal(p.puts.filter(p => p.url.endsWith('/audio')).length, 1, 'audio continues to carry what plays');
        });
    }
});

test('Next discards a response after its page was replaced, including visible fallbacks', async (t) => {
    for (const result of ['player', 'error']) await t.test(result, async (t) => {
        const p = fixture(t); let resolve;
        const next = p.create(() => new Promise(r => { resolve = r; }));
        const pending = next.go('button'); await Promise.resolve();
        p.navigateElsewhere();
        resolve(result === 'player' ? p.render() : null); await pending;
        assert.equal(p.mounts.length, 0);
        assert.equal(p.destroyed.length, 0);
        assert.equal(p.navigated.length, 0);
        assert.equal(p.w.location.pathname + p.w.location.search, '/ru/other?file=movie.mkv');
    });
});

test('disposing Next aborts the job, ignores late progress/results and prevents further starts', async (t) => {
    const p = fixture(t); let resolve, opts; const events = [];
    const next = p.create((form, o) => { opts = o; return new Promise(r => { resolve = r; }); }, { onEvent: (...args) => events.push(args) });
    const pending = next.go('button'); await Promise.resolve();
    next.dispose();
    assert.equal(opts.signal.aborted, true);
    const before = events.length;
    opts.onProgress('late');
    resolve(p.render('tr-pt', true)); await pending;
    await next.prepare(); await next.go('button');
    assert.equal(next.isPrepared(), false);
    assert.equal(events.length, before);
    assert.deepEqual([p.mounts.length, p.navigated.length, p.heads.length], [0, 0, 0]);
});

test('disposing Next while its token is pending sends no stream request', async (t) => {
    const p = fixture(t); let token; let fetched = 0;
    const next = p.create(async () => { fetched++; return p.render(); }, { getToken: () => new Promise(r => { token = r; }) });
    const pending = next.go('button');
    next.dispose(); token('late-token'); await pending;
    assert.equal(fetched, 0);
    assert.equal(p.navigated.length, 0);
});

test('Next requires both its connected host and the same stage after the wait', async (t) => {
    for (const change of ['host', 'stage']) await t.test(change, async (t) => {
        const p = fixture(t); let release;
        let stage = p.w.document.querySelector('.wt-player-stage');
        const next = p.create(() => new Promise(r => { release = r; }), { getStage: () => stage });
        const pending = next.go('button'); await Promise.resolve();
        if (change === 'host') stage.parentNode.remove();
        else { stage = p.w.document.createElement('div'); p.w.document.body.append(stage); }
        release(p.render()); await pending;
        assert.deepEqual([p.mounts.length, p.navigated.length, p.destroyed.length], [0, 0, 0]);
    });
});

test('navigation during an asynchronous mount prevents stale saves, history and fallback', async (t) => {
    for (const fail of [false, true]) await t.test(`mount rejects=${fail}`, async (t) => {
        const p = fixture(t);
        const next = p.create(async () => p.render('none', true), { initPlayer: async () => {
            p.navigateElsewhere();
            if (fail) throw new Error('navigation cancelled this mount');
        } });
        await next.go('button');
        assert.deepEqual(p.puts, []);
        assert.deepEqual(p.navigated, []);
        assert.equal(p.w.location.pathname + p.w.location.search, '/ru/other?file=movie.mkv');
    });
});

test('Next completes history and saves the carried choice when its own mount disposes the old player', async (t) => {
    const p = fixture(t); let next;
    next = p.create(async () => p.render('none', true), { destroyPlayer: () => next.dispose() });
    await next.go('button');
    assert.equal(p.mounts.length, 1);
    assert.equal(p.w.location.search, '?file=e02.mkv');
    assert.equal(p.puts.find(p => p.url.endsWith('/subtitle')).id, 'none');
});

test('Next re-prepares when subtitles, audio or the preferred language change after prewarming', async (t) => {
    for (const change of ['subtitle', 'audio', 'preferred']) await t.test(change, async (t) => {
        const p = fixture(t); let fetches = 0; const carries = [];
        select(p.w.document, 'os-os-en');
        const next = p.create(async (form) => {
            fetches++; const carry = Object.fromEntries(new p.w.FormData(form)); carries.push(carry);
            return p.render(carry['carry-sub'] === 'off' ? 'none' : 'os-os-en', true);
        });
        await next.prepare(); assert.equal(next.isPrepared(), true);
        if (change === 'subtitle') select(p.w.document, 'none');
        if (change === 'audio') select(p.w.document, 'mp-1', true, 'audio');
        if (change === 'preferred') p.w.document.querySelector('#subtitles').dataset.preferredLang = 'ru';
        assert.equal(next.isPrepared(), false);
        await next.go('button');
        assert.equal(fetches, 2);
        if (change === 'subtitle') assert.equal(p.mounts[0].selected, 'none');
        if (change === 'audio') assert.equal(carries[1]['carry-audio-lang'], 'en');
    });
});

test('Next rechecks a choice made during the cold start and does not warm its stale AI track', async (t) => {
    const p = fixture(t); let resolve, requests = 0;
    const next = p.create(async (form) => {
        if (++requests === 1) return new Promise(r => { resolve = r; });
        assert.equal(new p.w.FormData(form).get('carry-sub'), 'off');
        return p.render('none', true);
    });
    const pending = next.go('button'); await Promise.resolve();
    select(p.w.document, 'none'); resolve(p.render('tr-pt', true)); await pending;
    assert.equal(requests, 2);
    assert.equal(p.mounts[0].selected, 'none');
    assert.deepEqual(p.heads, []);
});

test('a replaced prewarm cannot overwrite a newer choice even when its transport ignores abort', async (t) => {
    const p = fixture(t); let release, signal; let requests = 0;
    const next = p.create(async (form, opts) => {
        if (++requests === 1) { signal = opts.signal; return new Promise(r => { release = r; }); }
        return p.render('none', true);
    });
    const old = next.prepare(); await Promise.resolve();
    select(p.w.document, 'none'); await next.prepare();
    assert.equal(signal.aborted, true);
    release(p.render('tr-pt', true)); await old;
    await next.go('button');
    assert.equal(requests, 2);
    assert.equal(p.mounts[0].selected, 'none');
    assert.deepEqual(p.heads, []);
});

test('an expired prewarm is refreshed before Next is pressed', async (t) => {
    const p = fixture(t); let now = 0, requests = 0;
    const next = p.create(async () => { requests++; return p.render(); }, { now: () => now });
    await next.prepare(); now += PREPARED_MAX_AGE_MS + 1;
    await next.prepare(); await next.go('button');
    assert.equal(requests, 2);
    assert.equal(p.mounts.length, 1);
});
