import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { JSDOM } from 'jsdom';
import { cssRules, findRule, color, hex, over, contrast } from '../test/css.mjs';

// The transfer status's and the Vault page's own style and markup held to
// what they must keep for a reader: text at 4.5:1 on the grounds it is drawn
// on (WCAG 1.4.3 -- all of it 10-14px), no meaning in colour alone, focus
// clear of what is fixed at the top, motion off for who asked for less.
// Read from the stylesheet itself and from the markup the Go tests generate
// from the real templates (__fixtures__).
const STYLE = cssRules(new URL('../../styles/style.css', import.meta.url));
const PLAYER = cssRules(new URL('../../styles/player.css', import.meta.url));
const W = createRequire(import.meta.url)('../../../../tailwind.config.js').theme.extend.colors.w;
const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8');
const PAGE = read('./__fixtures__/transfer-status-page.html');
const VAULT = read('./__fixtures__/vault-page.html');

const rule = (sel, at) => {
    const d = findRule(STYLE, sel, at);
    assert.ok(d, `a rule for ${sel}${at ? ' in ' + at.join(' ') : ''}`);
    return d;
};
const opacity = (sel, at) => parseFloat((findRule(STYLE, sel, at) || {}).opacity ?? '1');
const readable = (what, fg, bg) => {
    const c = contrast(fg, bg);
    assert.ok(c >= 4.5, `${what}: ${c.toFixed(2)}:1, under 4.5:1`);
};

// The grounds: the film card and the sticky bar (the block's --tx-bg), the
// player's cap card, the details popover; the Vault table is bg-base-200/50
// in a bg-base-300/50 card on the page (style.css --color-base-*: the w
// palette's surface, card and bg).
const CARD = hex(rule('.tx')['--tx-bg']);
const STICKY = hex(rule('#torrent-status-sticky .tx')['--tx-bg']);
const DETAILS = hex(rule('.tx-details').background);
const VAULT_TABLE = over(over(hex(W.bg), hex(W.card), 0.5), hex(W.surface), 0.5);
const pbox = (ground) => {
    const { rgb, a } = color(rule('.tx-pbox').background);
    return over(ground, rgb, a);
};

test('the plan box, the hint and the details read at 4.5:1 wherever they are drawn', () => {
    const playerCard = hex(findRule(PLAYER, '.wt-cap-card.tx-pbox').background);
    for (const [where, ground] of [['the card', pbox(CARD)], ['the sticky bar', pbox(STICKY)], ['the player\'s card', playerCard]]) {
        // "7 days free · cancel any time": the honest half of the offer.
        readable(`the trial note on ${where}`, hex(rule('.tx-pn').color), ground);
        readable(`the box's line on ${where}`, hex(rule('.tx-ps').color), ground);
    }
    for (const [where, ground] of [['the card', CARD], ['the sticky bar', STICKY]]) {
        readable(`the hint on ${where}`, hex(rule('.tx-hint').color), ground);
    }
    for (const sel of ['.tx-dt', '.tx-rlabel', '.tx-rsub', '.tx-rv']) {
        readable(`the details' ${sel}`, hex(rule(sel).color), DETAILS);
    }
});

// A tone's words and ground, from its rule: the w palette's utilities
// (tailwind.config.js) or the muted tone's own rgba. The tones from DaisyUI's
// and Tailwind's palettes are not resolved here -- of them only warn carries
// the swarm, 6.6:1 at 0.85 (2026-10-03).
const tone = (name) => {
    const d = rule(`.tx-badge[data-tone="${name}"]`);
    const text = d['@apply'].match(/text-w-(\w+)/);
    const bg = d['@apply'].match(/bg-w-(\w+)\/(\d+)/);
    return { fg: hex(W[text[1]]), bg: bg ? { rgb: hex(W[bg[1]]), a: bg[2] / 100 } : color(d['background-color']) };
};

test('a badge\'s swarm, "(14 seeders)", reads at 4.5:1 in its tone on every ground', () => {
    // Dimmed in one place, where this test reads it: not by a utility on
    // the partial's span.
    const span = read('../../../../templates/partials/status/badge.html').match(/class="tx-bx([^"]*)"/);
    assert.ok(span, 'the swarm\'s span');
    assert.doesNotMatch(span[1], /opacity/, 'the swarm\'s dimming is style.css .tx-bx');
    const dim = opacity('.tx-bx');
    for (const name of ['muted', 'cyan', 'vault', 'pink']) {
        const { fg, bg } = tone(name);
        for (const [where, ground] of [['the card', CARD], ['the sticky bar', STICKY], ['the Vault table', VAULT_TABLE]]) {
            const g = over(ground, bg.rgb, bg.a);
            readable(`${name} on ${where}`, over(g, fg, dim), g);
        }
    }
});

test('a dimmed node ("?", Vault waiting) dims its icon and outline, never its words', () => {
    // The node's opacity is the group's: words and pill both over the card.
    const a = opacity('.tx-node[data-dim]') * opacity('.tx-node[data-dim] .tx-pill');
    const pill = hex(rule('.tx-pill').background);
    for (const [where, ground] of [['the card', CARD], ['the sticky bar', STICKY]]) {
        const bg = over(ground, pill, a);
        readable(`"?" on ${where}`, over(ground, hex(rule('.tx-node').color), a), bg);
        readable(`the name on ${where}`, over(ground, hex(rule('.tx-nm').color), a), bg);
        readable(`Vault's value on ${where}`, over(ground, hex(rule('.tx-node[data-tone="vault"] .tx-pill').color), a), bg);
        readable(`the phone's caption on ${where}`, over(ground, hex(rule('.tx-node', ['@container']).color), a), ground);
    }
});

test('a pledge\'s "Expiring" says how long is left at 4.5:1 and 11px', () => {
    const doc = new JSDOM(VAULT).window.document;
    const td = doc.querySelector('[data-tone="pink"]').closest('td');
    const left = [...td.querySelectorAll('span')].find((s) => /text-\[\d+px\]/.test(s.className));
    assert.ok(left, 'the time left under the badge');
    const cls = left.className;
    assert.ok(parseInt(cls.match(/text-\[(\d+)px\]/)[1], 10) >= 11, `${cls}: 11px or more`);
    readable('the time left', hex(W[cls.match(/text-w-(\w+)/)[1]]), VAULT_TABLE);
});

test('the cap on a phone is a lock as well as pink: the compact chain has no "· cap"', () => {
    assert.equal(rule('.tx-note', ['@container']).display, 'none', 'what this guards: the word is gone there');
    assert.equal(rule('.tx-lk').display, 'none', 'the wide chain says "· cap" in words');
    const lk = rule('.tx-seg[data-tone="plan"] .tx-lk', ['@container']);
    assert.equal(lk.display, 'block');
    // At the start of the line, not before the speed: a 262 px block (a 320 px
    // phone) has 56 px for "5 Мбит/с", and the lock's 15 px there cut it to
    // "5 Мбит/(" (review 2026-10-03). The line makes room for it instead.
    assert.equal(lk.position, 'absolute', 'the lock takes no width from the speed');
    assert.ok(parseFloat(rule('.tx-seg[data-tone="plan"] .tx-ln', ['@container']).left) >= parseFloat(rule('.tx-lk').width), 'the line starts after the lock');
    const doc = new JSDOM(PAGE).window.document;
    assert.ok(doc.getElementById('tx-i-lock'), 'the lock in the block\'s sprite');
    const segs = doc.querySelectorAll('.tx-seg');
    assert.ok(segs.length > 0);
    for (const s of segs) {
        assert.equal(s.querySelector('.tx-lk use')?.getAttribute('href'), '#tx-i-lock', 'every segment has the lock (stable DOM: the tone shows it)');
        assert.equal(s.querySelector('.tx-lk').parentElement.className, 'tx-trk', 'on the track, out of the speed\'s label');
    }
});

test('keyboard focus does not scroll under the navbar', () => {
    const nav = read('../../../../templates/partials/nav.html').match(/navbar-redesign fixed[^"]*\bh-\[(\d+)px\]/);
    assert.ok(nav, 'the navbar\'s height');
    assert.equal(rule('html')['scroll-padding-top'], `${nav[1]}px`);
});

test('a Vault row\'s badge wraps in its column instead of hiding the state in a title', () => {
    assert.equal(rule('#vault-pledges .tx-badge .badge-text')['white-space'], 'normal');
});

test('the Vault status guide is a list of terms and their meanings', () => {
    const doc = new JSDOM(VAULT).window.document;
    const groups = doc.querySelectorAll('dl > div');
    assert.ok(groups.length >= 3);
    for (const g of groups) {
        assert.equal(g.querySelectorAll(':scope > dt').length, 1, `a term: ${g.textContent.trim().slice(0, 40)}`);
        assert.equal(g.querySelectorAll(':scope > dd').length, 1);
    }
});

test('nothing of the status or the Vault rows moves for a viewer who asked for less motion', () => {
    const still = new Set(STYLE
        .filter((r) => r.at.some((a) => a.includes('prefers-reduced-motion')) && r.decls.animation === 'none')
        .flatMap((r) => r.sel.split(',').map((s) => s.trim())));
    const moving = STYLE.filter((r) => !r.at.some((a) => a.includes('prefers-reduced-motion') || a.startsWith('@keyframes'))
        && /\.(tx|vault)-/.test(r.sel) && r.decls.animation && r.decls.animation !== 'none');
    assert.ok(moving.length > 0);
    for (const r of moving) {
        for (const sel of r.sel.split(',').map((s) => s.trim())) assert.ok(still.has(sel), `${sel} keeps its animation`);
    }
});

test('the details\' × is a finger\'s size on a touch screen, as the box\'s', () => {
    const coarse = rule('.tx-dx', ['pointer: coarse']);
    assert.equal(coarse.width, '44px');
    assert.equal(coarse.height, '44px');
});

test('the box\'s × says what it does: the offer goes for a day, on every torrent', () => {
    const doc = new JSDOM(PAGE).window.document;
    const ru = JSON.parse(read('../../../../locales/ru.json'));
    const x = doc.querySelector('[data-tx-pclose]');
    assert.equal(x.getAttribute('aria-label'), ru['resource.status.dismissBox']);
    assert.notEqual(x.getAttribute('aria-label'), doc.querySelector('[data-tx-close]').getAttribute('aria-label'), 'not the popover\'s "Close"');
});

test('opening the details is counted', () => {
    const doc = new JSDOM(PAGE).window.document;
    assert.equal(doc.querySelector('.tx-chain').getAttribute('data-umami-event'), 'status-details');
});

test('without JS "Checking activity…" stands still: its dots are the reduced-motion ones', () => {
    // The page's render is "checking" (statusview Pending) and only the
    // stream moves it on: without JS the badge says so for good, and the
    // running dots would claim a check that never comes.
    const doc = new JSDOM(PAGE).window.document;
    const flat = (s) => s.replace(/\s*([{}:;])\s*/g, '$1').replace(/\s+/g, ' ').trim();
    const css = flat([...doc.querySelectorAll('noscript style')].map((s) => s.textContent).join(''));
    for (const sel of ['.tx-badge[data-icon="dots"] .tx-bdots', '.tx-badge[data-icon="dots"] .tx-bi']) {
        const d = rule(sel, ['prefers-reduced-motion']);
        assert.ok(css.includes(`${sel}{display:${d.display}}`), `${sel} as under reduced motion (display: ${d.display}) in the block's noscript: ${css}`);
    }
    assert.equal(doc.querySelectorAll('.tx noscript').length, 0, 'outside .tx: the sticky bar clones .tx');
});

test('the sticky bar is not a status region: no empty role around buttons, nothing live', () => {
    // role="status" switched off by aria-live="off" was a role with no
    // purpose over interactive content; what matters is said once, by the
    // card block's own region outside the .tx the bar clones.
    const doc = new JSDOM(PAGE).window.document;
    const bar = doc.getElementById('torrent-status-sticky');
    assert.ok(bar, 'the sticky bar in the page');
    assert.equal(bar.getAttribute('role'), null);
    assert.equal(bar.getAttribute('aria-live'), null);
    const said = doc.querySelectorAll('[data-tx-announce]');
    assert.equal(said.length, 1, 'one region says it');
    assert.equal(said[0].closest('.tx, #torrent-status-sticky'), null);
});
