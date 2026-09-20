import test from 'node:test';
import assert from 'node:assert/strict';
import { bindMediaSession } from './media-session.js';

function fakeSession({ unsupported = [] } = {}) {
    const s = {
        handlers: {}, positions: [], metadata: undefined,
        setActionHandler(a, fn) { if (unsupported.includes(a)) throw new TypeError('unsupported'); s.handlers[a] = fn; },
        setPositionState(p) { s.positions.push(p); },
    };
    return s;
}
class Meta { constructor(init) { Object.assign(this, init); } }

function bind(over = {}) {
    const calls = [];
    let pos = { currentTime: 1830, duration: 6000, rate: 1 };
    const session = over.session || fakeSession();
    const ms = bindMediaSession({
        session, Metadata: Meta, title: 'Silo S01E02', artwork: 'https://x/p.jpg',
        onPlay: () => calls.push('play'), onPause: () => calls.push('pause'),
        onSeekTo: (t) => calls.push('seek:' + t), getPosition: () => pos,
    });
    return { ms, session, calls, setPos: (p) => { pos = { ...pos, ...p }; } };
}

test('the lock screen gets the film, not the page', () => {
    const h = bind();
    assert.equal(h.session.metadata.title, 'Silo S01E02');
    assert.deepEqual(h.session.metadata.artwork, [{ src: 'https://x/p.jpg' }]);
});

test('seeking goes through the player, in film time, clamped', () => {
    const h = bind();
    h.session.handlers.seekforward({});
    h.session.handlers.seekbackward({ seekOffset: 30 });
    h.session.handlers.seekto({ seekTime: 99999 });
    h.session.handlers.seekto({});
    h.setPos({ currentTime: 3 });
    h.session.handlers.seekbackward({});
    assert.deepEqual(h.calls, ['seek:1840', 'seek:1800', 'seek:6000', 'seek:0']);
    h.session.handlers.play(); h.session.handlers.pause();
    assert.deepEqual(h.calls.slice(-2), ['play', 'pause']);
});

test('position state is film time -- a run that starts at 30:00 is not at 0:30', () => {
    const h = bind();
    h.ms.update();
    assert.deepEqual(h.session.positions[0], { duration: 6000, playbackRate: 1, position: 1830 });
    h.setPos({ rate: 1.5, currentTime: 7000 });
    h.ms.update();
    assert.deepEqual(h.session.positions[1], { duration: 6000, playbackRate: 1.5, position: 6000 }, 'never past the end: setPositionState throws on that');
    h.setPos({ duration: 0 });
    h.ms.update();
    assert.equal(h.session.positions.length, 2, 'an unknown duration reports nothing');
});

test('an action the browser does not know costs only that action', () => {
    const h = bind({ session: fakeSession({ unsupported: ['seekto'] }) });
    assert.equal(typeof h.session.handlers.play, 'function');
    assert.equal(h.session.handlers.seekto, undefined);
    h.ms.destroy();
    assert.equal(h.session.handlers.play, null, 'handlers are handed back');
    assert.equal(h.session.metadata, null);
});

test('no Media Session API: a no-op, not a throw', () => {
    const ms = bindMediaSession({ session: undefined });
    ms.update(); ms.destroy();
});
