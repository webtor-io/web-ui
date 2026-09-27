import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { capLock } from './buffering-label.js';

// The label the transfer status publishes while the viewer is held at the
// cap (lib/transferStatus.js playerLabel), in the shape the lock needs.
const LABEL = {
    rate: '5 Мбит/с',
    title: 'Видео подгружается медленнее, чем играет',
    sub: 'Без подписки — до 5 Мбит/с',
    cta: { label: 'Смотреть без ограничения скорости', note: '', url: '/ru/trial?from=player-label' },
    props: {},
};
// A wait on screen, past any grace window.
const WAIT = { label: LABEL, graceSec: 600, movieTime: 700 };

test('a wait with the status\'s word on the cap: the lock', () => {
    assert.equal(capLock(WAIT), LABEL);
    assert.equal(capLock({ ...WAIT, graceSec: 0, movieTime: 5 }), LABEL, 'no grace window at all');
    assert.equal(capLock({ ...WAIT, movieTime: 600 }), LABEL, 'the window is over at its end');
});

test('no word on the cap, or not a whole one: the plain pill', () => {
    assert.equal(capLock({ ...WAIT, label: null }), null, 'the swarm, the network, an embed');
    assert.equal(capLock({ ...WAIT, label: { ...LABEL, rate: '' } }), null, 'no rate to say');
    assert.equal(capLock({ ...WAIT, label: { ...LABEL, cta: null } }), null, 'no card to open');
    assert.equal(capLock({ ...WAIT, label: { ...LABEL, cta: { ...LABEL.cta, url: '' } } }), null, 'no link on the card');
    assert.equal(capLock(), null);
});

test('inside the free grace window by movie time: the plain pill, whatever the word', () => {
    assert.equal(capLock({ ...WAIT, movieTime: 30 }), null);
    assert.equal(capLock({ ...WAIT, movieTime: 599.9 }), null, 'a session seek back into the window');
});

test('the grace popup up, or coming up this render: the plain pill', () => {
    assert.equal(capLock({ ...WAIT, graceUp: true }), null);
});

// ---- the pill's colours (player.css) ---------------------------------------
//
// Owner, 2026-09-26: on a white frame the lock's pink hover fill turned the
// pill into a light-pink blob, its white "Buffering" gone. The pill is dark
// in every state; hovered or open it only lightens a little. Read from the
// stylesheet itself, and the contrast computed over a pure white frame --
// the worst frame there is for white text on a translucent pill.

const CSS = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), '../../../styles/player.css'), 'utf8')
    .replace(/\/\*[\s\S]*?\*\//g, '');
// Every innermost rule (those inside @media included): [selector,
// { property: value }]; and those whose selector names the pill.
const rules = [...CSS.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
    .map(([, sel, body]) => [sel.trim().replace(/\s+/g, ' '), Object.fromEntries(body.split(';')
        .map((d) => d.split(':'))
        .filter((kv) => kv.length >= 2)
        .map(([k, ...v]) => [k.trim(), v.join(':').trim()]))]);
const pillRules = rules.filter(([sel]) => sel.includes('wt-buffering-pill'));
// The first rule whose selector list has exactly `sel`: the one at the top
// level (the @media ones come after it in the file).
const rule = (sel) => {
    const r = rules.find(([s]) => s.split(',').map((x) => x.trim()).includes(sel));
    assert.ok(r, `a rule for ${sel}`);
    return r[1];
};

// The layers of a background, bottom first: the colour, and every flat
// gradient over it (one colour at every stop) as a layer of that colour.
const rgba = (s) => {
    const [r, g, b, a = 1] = s.slice(s.indexOf('(') + 1, -1).split(',').map((x) => parseFloat(x));
    return { rgb: [r, g, b], a };
};
const layers = (bg) => {
    // Split at the commas outside parentheses.
    const parts = [];
    let depth = 0;
    let cur = '';
    for (const ch of bg) {
        if (ch === '(') depth++;
        if (ch === ')') depth--;
        if (ch === ',' && depth === 0) {
            parts.push(cur.trim());
            cur = '';
        } else {
            cur += ch;
        }
    }
    parts.push(cur.trim());
    return parts.map((part) => {
        const stops = part.match(/rgba?\([^)]*\)/g) || [];
        assert.ok(stops.length && stops.every((x) => x.replace(/\s/g, '') === stops[0].replace(/\s/g, '')), `a flat layer: ${part}`);
        return rgba(stops[0]);
    }).reverse();
};
const over = (frame, ls) => ls.reduce((under, { rgb, a }) => rgb.map((c, i) => a * c + (1 - a) * under[i]), frame);
const lum = (rgb) => {
    const lin = (c) => { const v = c / 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
    const [r, g, b] = rgb.map(lin);
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const contrast = (a, b) => {
    const [x, y] = [lum(a), lum(b)];
    return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
};
const hex = (h) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
const WHITE = [255, 255, 255];
const PINKS = /232,\s*67,\s*147|253,\s*121,\s*168|#fd79a8|#e84393/i;

test('the pill is dark in every state: no pink fill anywhere on it', () => {
    for (const [sel, decls] of pillRules) {
        for (const prop of ['background', 'background-color', 'background-image']) {
            if (decls[prop]) assert.doesNotMatch(decls[prop], PINKS, `${sel} ${prop}`);
        }
    }
    const hover = rule('button.wt-buffering-pill:hover');
    assert.deepEqual(rule('button.wt-buffering-pill[aria-expanded="true"]'), hover, 'open looks as hovered');
    const base = layers(rule('.wt-buffering-pill').background);
    const lit = layers(hover.background);
    assert.deepEqual(lit[0], base[0], 'hovered: the same dark base under it');
    assert.ok(lit.slice(1).every(({ rgb }) => rgb.every((c) => c === 255)), 'and only white over it: lighter, not another colour');
    assert.ok(lit.slice(1).reduce((s, { a }) => s + a, 0) <= 0.12, 'a little lighter');
});

test('on a pure white frame the words and the cap read at 4.5:1 or better, hovered too', () => {
    const text = hex(rule('.wt-buffering-pill').color);
    const pink = hex(rule('.wt-buffering-cap').color);
    for (const [state, sel] of [['at rest', '.wt-buffering-pill'], ['hovered or open', 'button.wt-buffering-pill:hover']]) {
        const bg = over(WHITE, layers(rule(sel).background));
        assert.ok(contrast(text, bg) >= 4.5, `${state}: "Buffering" ${contrast(text, bg).toFixed(2)}:1`);
        assert.ok(contrast(pink, bg) >= 4.5, `${state}: the cap ${contrast(pink, bg).toFixed(2)}:1`);
    }
});

test('the lock shows the keyboard\'s focus', () => {
    const ring = rule('button.wt-buffering-pill:focus-visible');
    assert.match(ring.outline, /^2px solid /);
    assert.ok(ring['box-shadow'], 'on a dark halo, seen on a white frame too');
});
