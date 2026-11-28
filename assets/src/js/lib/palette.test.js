import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { hex, contrast } from '../test/css.mjs';

// w-muted is the quietest text the site sets, most of it at 12px: it keeps
// AA's 4.5:1 on every ground it is drawn on -- w-card (modals, poster cards)
// the lightest -- and stays under w-sub, which stays under w-text.
const W = createRequire(import.meta.url)('../../../../tailwind.config.js').theme.extend.colors.w;

test('w-muted reads at 4.5:1 on the page, the surface and the card, and stays the quietest', () => {
    for (const ground of ['bg', 'surface', 'card']) {
        const c = contrast(hex(W.muted), hex(W[ground]));
        assert.ok(c >= 4.5, `w-muted on w-${ground}: ${c.toFixed(2)}:1`);
    }
    const [muted, sub, text] = ['muted', 'sub', 'text'].map((k) => contrast(hex(W[k]), hex(W.card)));
    assert.ok(muted < sub && sub < text, `on w-card: muted ${muted.toFixed(2)}, sub ${sub.toFixed(2)}, text ${text.toFixed(2)}`);
});

// Rules a utility cannot reach (a DaisyUI override, a nested selector) spell
// the colour out; a change of the token has to reach them too.
test('style.css spells w-muted as the token has it', () => {
    const css = readFileSync(new URL('../../styles/style.css', import.meta.url), 'utf8');
    const copies = [...css.matchAll(/(#[0-9a-f]{6})(?: !important)?; \/\* w-muted \*\//g)].map((m) => m[1]);
    assert.ok(copies.length >= 2, `copies found: ${copies.length}`);
    for (const c of copies) assert.equal(c, W.muted);
});
