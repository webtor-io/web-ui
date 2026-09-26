import test from 'node:test';
import assert from 'node:assert/strict';
import { capLock } from './buffering-label.js';

// The label the transfer status publishes at a stall at the cap
// (lib/transferStatus.js playerLabel), in the shape the lock needs.
const LABEL = {
    rate: '5 Мбит/с',
    title: 'Видео подгружается медленнее, чем играет',
    sub: 'Без подписки — до 5 Мбит/с',
    cta: { label: 'Смотреть без ограничения скорости', note: '', url: '/ru/trial?from=player-label' },
    props: {},
};
// The playing film stalls, past any grace window.
const STALL = { label: LABEL, playing: true, loading: true, graceSec: 600, movieTime: 700 };

test('a stall of the playing film with the status\'s word on the cap: the lock', () => {
    assert.equal(capLock(STALL), LABEL);
    assert.equal(capLock({ ...STALL, graceSec: 0, movieTime: 5 }), LABEL, 'no grace window at all');
    assert.equal(capLock({ ...STALL, movieTime: 600 }), LABEL, 'the window is over at its end');
});

test('no word on the cap, or not a whole one: the plain pill', () => {
    assert.equal(capLock({ ...STALL, label: null }), null, 'the swarm, the network, an embed');
    assert.equal(capLock({ ...STALL, label: { ...LABEL, rate: '' } }), null, 'no rate to say');
    assert.equal(capLock({ ...STALL, label: { ...LABEL, cta: null } }), null, 'no card to open');
    assert.equal(capLock({ ...STALL, label: { ...LABEL, cta: { ...LABEL.cta, url: '' } } }), null, 'no link on the card');
    assert.equal(capLock(), null);
});

test('only a stall of the playing film: not a start, a seek, a hold or the next file', () => {
    assert.equal(capLock({ ...STALL, loading: false }), null, 'playing');
    assert.equal(capLock({ ...STALL, playing: false }), null, 'paused (the grace popup holds it paused too)');
    assert.equal(capLock({ ...STALL, seeking: true }), null, 'a session seek');
    assert.equal(capLock({ ...STALL, preHolding: true }), null, 'the translation hold');
    assert.equal(capLock({ ...STALL, nextLoading: true }), null, 'the next file loading');
    assert.equal(capLock({ ...STALL, awaitingStart: true }), null, 'a player moved to, before its first frame');
});

test('inside the free grace window by movie time: the plain pill, whatever the word', () => {
    assert.equal(capLock({ ...STALL, movieTime: 30 }), null);
    assert.equal(capLock({ ...STALL, movieTime: 599.9 }), null);
});
