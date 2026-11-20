// A toast goes after 3 s unless the caller says how long it stays.
import test, { after } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body><div id="toast-container"></div></body>');
const prev = [global.window, global.document, global.requestAnimationFrame];
global.window = dom.window;
global.document = dom.window.document;
// The module calls the bare name; jsdom has none without pretendToBeVisual.
global.requestAnimationFrame = (fn) => fn();
after(() => { [global.window, global.document, global.requestAnimationFrame] = prev; });

test('a toast lives 3 s unless told otherwise', async (t) => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    const { default: toast } = await import('./toast.js');
    const box = document.getElementById('toast-container');
    // tick moves the clock by each step in turn: the module's 300 ms
    // fallback is set when the first timer fires, so it counts from there.
    const tick = (...steps) => steps.forEach((ms) => t.mock.timers.tick(ms));

    toast.error('a');
    assert.equal(box.children.length, 1);
    assert.equal(box.textContent, 'a');
    tick(2999);
    assert.equal(box.children.length, 1);
    tick(1, 300);
    assert.equal(box.children.length, 0);

    toast.error('b', 10000);
    tick(3000, 300);
    assert.equal(box.children.length, 1);
    tick(6699);
    assert.equal(box.children.length, 1);
    tick(1, 300);
    assert.equal(box.children.length, 0);

    toast.success('c', 10000);
    toast.info('d', 10000);
    tick(3000, 300);
    assert.equal(box.children.length, 2);
    tick(6700, 300);
    assert.equal(box.children.length, 0);

    toast.success('e');
    tick(3000, 300);
    assert.equal(box.children.length, 0);
});
