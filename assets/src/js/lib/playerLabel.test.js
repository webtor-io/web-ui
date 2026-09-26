import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

// The status view (app/resource/status.js) and the player (the stream
// action's chunk) each bundle their own copy of lib/playerLabel.js: what one
// publishes, the other must read. A second import stands in for the second
// bundle (CLAUDE.md, shared JS state).

const dom = new JSDOM('<!doctype html><html><body></body></html>');
global.window = dom.window;
global.document = dom.window.document;

const status = await import('./playerLabel.js');
const player = await import('./playerLabel.js?bundle=two');

test('two copies of the module, one label: the player reads what the status published', () => {
    assert.notEqual(status.publishPlayerLabel, player.publishPlayerLabel, 'fixture: two module instances');
    const seen = [];
    const stop = player.onPlayerLabel((l) => seen.push(l));
    const label = { rate: '5 Мбит/с', cta: { url: '/trial?from=player-label' } };
    assert.equal(status.publishPlayerLabel(label), true);
    assert.deepEqual(player.currentPlayerLabel(), label, 'a player mounted later reads it as it stands');
    assert.deepEqual(seen, [label], 'a mounted player is told');
    // The same label again (the status draws every second): not told again.
    assert.equal(status.publishPlayerLabel({ rate: '5 Мбит/с', cta: { url: '/trial?from=player-label' } }), false);
    assert.equal(seen.length, 1);
    assert.equal(status.publishPlayerLabel(null), true);
    assert.equal(player.currentPlayerLabel(), null);
    assert.deepEqual(seen, [label, null]);
    assert.equal(status.publishPlayerLabel(undefined), false, 'none is none');
    stop();
    status.publishPlayerLabel(label);
    assert.equal(seen.length, 2, 'unsubscribed');
    status.publishPlayerLabel(null);
});
