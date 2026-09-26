import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';

// The status badge's one renderer (lib/statusBadge.js) against the server's
// own markup: the resource page's block and the Vault page, both generated
// by Go tests from the real partial (partials/status/badge.html), and the
// Vault dashboard's stream messages generated through the handler's own
// presentation (handlers/resource TestVaultStatusFixturesAreCurrent), each
// with the partial's render of its badge.
const RESOURCE = readFileSync(new URL('./__fixtures__/transfer-status-page.html', import.meta.url), 'utf8');
const VAULT = readFileSync(new URL('./__fixtures__/vault-page.html', import.meta.url), 'utf8');
const STATES = JSON.parse(readFileSync(new URL('./__fixtures__/vault-status-states.json', import.meta.url), 'utf8'));

const dom = new JSDOM('<!doctype html><html lang="ru"><body></body></html>', { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;

const { bindBadge, applyBadge } = await import('./statusBadge.js');

const fresh = (html) => new JSDOM(`<!doctype html><body>${html}</body>`).window.document.querySelector('[data-tx-badge]');

// Everything a viewer sees or a script reads of a badge.
function snapshot(el) {
    return {
        tone: el.getAttribute('data-tone'),
        icon: el.getAttribute('data-icon'),
        pulse: el.hasAttribute('data-pulse'),
        use: el.querySelector('.tx-bi use').getAttribute('href'),
        label: el.querySelector('[data-tx-blabel]').textContent,
        extra: el.querySelector('[data-tx-bextra]').textContent,
        // The words' whole text, for a badge its column cuts.
        title: el.querySelector('.badge-text').getAttribute('title'),
        text: el.textContent.replace(/\s+/g, ' ').trim(),
    };
}

// The element's shape: tags, classes and which attributes it has, not their
// values (those are the state's), all the way down.
function shape(el) {
    const attrs = el.getAttributeNames().filter((n) => n !== 'id').sort();
    return {
        tag: el.tagName,
        cls: el.getAttribute('class'),
        attrs: attrs.filter((n) => !['data-tone', 'data-icon', 'data-pulse', 'href'].includes(n)),
        kids: Array.from(el.children).map(shape),
    };
}

function vaultBadge() {
    document.body.innerHTML = VAULT;
    return document.querySelector('[data-vault-progress] [data-tx-badge]');
}

test('the resource page\'s badge and every pill on the Vault page are one element', () => {
    document.body.innerHTML = RESOURCE;
    const resource = document.querySelector('#torrent-status [data-tx-badge]');
    document.body.innerHTML = VAULT;
    const pills = Array.from(document.querySelectorAll('.badge-sm'));
    assert.equal(pills.length, 6, 'the four rows and the guide\'s two');
    for (const el of pills) {
        assert.ok(el.hasAttribute('data-tx-badge'), `a pill of another markup: ${el.outerHTML}`);
        assert.deepEqual(shape(el), shape(resource));
    }
    // The resource page's describes its chain; the Vault page's are many, and
    // an id would repeat.
    assert.equal(resource.querySelector('.badge-text').id, 'tx-bdesc');
    assert.equal(document.querySelectorAll('[data-tx-badge] [id]').length, 0);
});

test('every state: the badge updated in place ends where a fresh server render would', () => {
    assert.ok(STATES.length >= 12, `${STATES.length} states`);
    for (const st of STATES) {
        const el = vaultBadge();
        applyBadge(bindBadge(el), st.status.badge);
        assert.deepEqual(snapshot(el), snapshot(fresh(st.ssr)), st.design);
    }
});

// The words never wrap, and a phone's Vault column cuts most states: every
// badge -- the server's and one updated in place -- carries its whole text
// as the words' title, and only there (the badge itself takes focus; a title
// on it would be read out after the words).
test('the whole text is the words\' title, on the server\'s badge and after every update', () => {
    const whole = (el) => el.querySelector('.badge-text').textContent.replace(/\s+/g, ' ').trim();
    document.body.innerHTML = RESOURCE + VAULT;
    for (const el of document.querySelectorAll('[data-tx-badge]')) {
        assert.equal(el.querySelector('.badge-text').getAttribute('title'), whole(el), el.outerHTML);
        assert.equal(el.hasAttribute('title'), false, 'not on the focusable badge');
    }
    const el = vaultBadge();
    const refs = bindBadge(el);
    for (const st of STATES) {
        applyBadge(refs, st.status.badge);
        assert.equal(el.querySelector('.badge-text').getAttribute('title'), whole(el), st.design);
    }
    applyBadge(refs, { tone: 'vault', icon: 'up', label: 'Сохраняется 64%', extra: '(9 сидов)' });
    assert.equal(refs.words.getAttribute('title'), 'Сохраняется 64% (9 сидов)');
    applyBadge(refs, { tone: 'vault', icon: 'vault', label: 'Сохранён' });
    assert.equal(refs.words.getAttribute('title'), 'Сохранён', 'no swarm left behind');
});

test('through every state and back: the same nodes, never an element added or removed', async () => {
    const el = vaultBadge();
    const refs = bindBadge(el);
    assert.equal(bindBadge(el), refs, 'bound once');
    const before = [el, ...el.querySelectorAll('*')];
    const records = [];
    const mo = new dom.window.MutationObserver((r) => records.push(...r));
    mo.observe(el, { childList: true, subtree: true });
    for (const st of [...STATES, ...STATES.slice().reverse()]) {
        applyBadge(refs, st.status.badge);
        await Promise.resolve();
        const now = [el, ...el.querySelectorAll('*')];
        assert.ok(now.length === before.length && now.every((n, i) => n === before[i]), `${st.design}: the same elements`);
    }
    mo.disconnect();
    const moved = records.filter((r) => [...r.addedNodes, ...r.removedNodes].some((n) => n.nodeType === 1));
    assert.deepEqual(moved, [], 'no element added or removed');
    // Once the words have a text node, a change is that node's.
    const label = refs.label.firstChild;
    applyBadge(refs, STATES[2].status.badge);
    applyBadge(refs, STATES[0].status.badge);
    assert.equal(refs.label.firstChild, label, 'the same text node');
    assert.equal(label.data, STATES[0].status.badge.label);
});

test('a badge that repeats writes nothing', async () => {
    const el = vaultBadge();
    const refs = bindBadge(el);
    applyBadge(refs, STATES[0].status.badge);
    const records = [];
    const mo = new dom.window.MutationObserver((r) => records.push(...r));
    mo.observe(el, { attributes: true, childList: true, characterData: true, subtree: true });
    applyBadge(refs, { ...STATES[0].status.badge });
    await Promise.resolve();
    mo.disconnect();
    assert.deepEqual(records, []);
});

test('pulse and extra come and go; nothing to draw is no change', () => {
    const el = vaultBadge();
    const refs = bindBadge(el);
    applyBadge(refs, { tone: 'vault', icon: 'up', pulse: true, label: 'Сохраняется 64%', extra: '(9 сидов)' });
    assert.equal(el.hasAttribute('data-pulse'), true);
    assert.equal(snapshot(el).text, 'Сохраняется 64% (9 сидов)');
    applyBadge(refs, { tone: 'vault', icon: 'vault', label: 'Сохранён' });
    assert.equal(el.hasAttribute('data-pulse'), false);
    assert.equal(refs.extra.textContent, '');
    assert.equal(el.querySelector('use').getAttribute('href'), '#tx-b-vault');
    applyBadge(refs, null);
    applyBadge(null, { tone: 'err' });
    assert.equal(el.getAttribute('data-tone'), 'vault');
    assert.equal(bindBadge(null), null);
});

// Every tone and icon a badge can arrive with -- the server's for every state
// of the resource page and of the Vault dashboard, and the Vault page's own
// pills -- has its colour in style.css and its glyph in the sprite: a tone
// without a rule is a badge with no colour at all.
test('every tone has its colour, every icon its glyph', () => {
    const css = readFileSync(new URL('../../styles/style.css', import.meta.url), 'utf8');
    const PAGE_STATES = JSON.parse(readFileSync(new URL('./__fixtures__/transfer-status-states.json', import.meta.url), 'utf8'));
    document.body.innerHTML = VAULT;
    const badges = [
        ...STATES.map((s) => s.status.badge),
        ...PAGE_STATES.map((s) => s.status.view.badge),
        ...Array.from(document.querySelectorAll('[data-tx-badge]')).map((el) => ({ tone: el.dataset.tone, icon: el.dataset.icon })),
    ];
    const tones = new Set(badges.map((b) => b.tone));
    const icons = new Set(badges.map((b) => b.icon));
    assert.ok(tones.has('pink') && tones.has('vault') && tones.has('muted'), [...tones].join());
    for (const tone of tones) assert.ok(css.includes(`.tx-badge[data-tone="${tone}"]`), `no colour for tone ${tone}`);
    for (const icon of icons) assert.ok(document.getElementById(`tx-b-${icon}`), `no glyph for icon ${icon}`);
});
