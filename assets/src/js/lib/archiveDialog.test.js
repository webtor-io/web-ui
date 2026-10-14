// The TAR/ZIP forms of a directory (templates/partials/list.html) open the
// "Preparing download" dialog from their own onclick. The markup is the
// server's (see toolIntent.test.js), inline handlers included.
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';

function page() {
    const html = readFileSync(new URL('./__fixtures__/tool-intent-multi.html', import.meta.url), 'utf8');
    const dom = new JSDOM(`<!doctype html><body>${html}</body>`, { url: 'https://webtor.io/', runScripts: 'dangerously' });
    const doc = dom.window.document;
    doc.addEventListener('submit', (e) => e.preventDefault());
    const opened = [];
    dom.window.HTMLDialogElement.prototype.showModal = function () { opened.push(this.id); };
    const forms = [...doc.querySelectorAll('form.js-archive-form')];
    const click = (el) => el.dispatchEvent(new dom.window.MouseEvent('click', { bubbles: true }));
    return { forms, opened, click };
}

test('a press on TAR or ZIP opens the dialog', () => {
    const { forms, opened, click } = page();
    assert.equal(forms.length, 2, 'TAR and ZIP');
    for (const f of forms) click(f.querySelector('button'));
    assert.equal(opened.length, 2);
});

test('a click on the ZIP menu item beside its button opens nothing (nothing is submitted)', () => {
    // DaisyUI pads a .menu item's child: the form is a larger box than the
    // button, and a click on that padding never submits.
    const { forms, opened, click } = page();
    for (const f of forms) click(f);
    assert.deepEqual(opened, []);
});

test('a press on a busy button (it lands on the form) reopens the dialog', () => {
    const { forms, opened, click } = page();
    for (const f of forms) {
        f.dataset.busy = 'true';
        click(f);
    }
    assert.equal(opened.length, 2);
});
