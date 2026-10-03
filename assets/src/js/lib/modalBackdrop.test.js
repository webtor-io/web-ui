import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

// DaisyUI's modal backdrop is a form with a button the size of the screen,
// transparent, labelled "close" in English: a click beside the card closes
// it. For the keyboard it was an invisible Tab stop that a screen reader
// read in English -- and every such dialog has its × and Esc. So it is
// mouse-only: out of the Tab order and out of the accessibility tree.
const ROOT = new URL('../../../../', import.meta.url);
const files = (dir, ext) => readdirSync(new URL(dir, ROOT), { recursive: true })
    .filter((f) => f.endsWith(ext) && !f.includes('__fixtures__') && !f.endsWith('.test.js'))
    .map((f) => dir + f);

test('a modal\'s backdrop button is neither a Tab stop nor read out', () => {
    const found = [];
    for (const f of [...files('templates/', '.html'), ...files('assets/src/js/', '.jsx')]) {
        const src = readFileSync(new URL(f, ROOT), 'utf8');
        for (const m of src.matchAll(/class="modal-backdrop"[^>]*>\s*<button([^>]*)>/g)) {
            found.push(f);
            assert.match(m[1], /tabindex="-1"/, `${f}: the backdrop's button is a Tab stop`);
            assert.match(m[1], /aria-hidden="true"/, `${f}: the backdrop's button is read out`);
        }
    }
    assert.ok(found.length >= 10, `the backdrops found: ${found.length}`);
});
