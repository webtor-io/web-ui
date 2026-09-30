import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createVodGuard, isVodStream, composeGuards, VOD_CODEC_REFUSALS } from './vod-guard.js';

const fatal = (details, extra = {}) => ({ type: 'mediaError', details, fatal: true, ...extra });
const fakeHls = () => ({ stops: 0, stopLoad() { this.stops++; } });

test('isVodStream: nginx-vod\'s URL, not the transcoder\'s or a file', () => {
    assert.equal(isVodStream('https://x.test/h/a.mp4~vod/hls/54e2/index.m3u8?token=T'), true);
    assert.equal(isVodStream('https://x.test/h/a.mkv~hls/session/abc/index.m3u8'), false);
    assert.equal(isVodStream('https://x.test/h/a.mp4'), false);
    assert.equal(isVodStream(''), false);
    assert.equal(isVodStream(undefined), false);
});

// The repro: addSourceBuffer refused, fatal with one level. Given up once:
// loading stopped, told once with the refused type; every later fatal error
// is taken too, so hls-manager recovers nothing.
test('a codec refusal is given up once, and every later fatal error is taken', () => {
    const told = [];
    const g = createVodGuard({ giveUp: (d, m) => told.push([d, m]) });
    const hls = fakeHls();
    assert.equal(g.onHlsError(hls, fatal('bufferAddCodecError', { mimeType: 'video/mp4;codecs=ec-3,hev1.2.4.H150.B0' })), true);
    assert.equal(g.done, true);
    assert.equal(hls.stops, 1);
    assert.deepEqual(told, [['bufferAddCodecError', 'video/mp4;codecs=ec-3,hev1.2.4.H150.B0']]);
    assert.equal(g.onHlsError(hls, fatal('bufferAddCodecError')), true, 'the next is taken');
    assert.equal(g.onHlsError(hls, fatal('bufferAppendError')), true, 'any fatal one after');
    assert.equal(told.length, 1, 'told once');
});

test('both refusals hls.js raises are given up', () => {
    for (const details of VOD_CODEC_REFUSALS) {
        const told = [];
        const g = createVodGuard({ giveUp: (d) => told.push(d) });
        assert.equal(g.onHlsError(fakeHls(), fatal(details)), true, details);
        assert.deepEqual(told, [details]);
    }
});

// Anything else is hls-manager's, as before: a fatal media error that is not
// a refusal, a non-fatal refusal (hls.js may switch the level itself), and
// nothing after a non-fatal one.
test('not a refusal, or not fatal: hls-manager\'s', () => {
    const told = [];
    const g = createVodGuard({ giveUp: (d) => told.push(d) });
    const hls = fakeHls();
    assert.equal(g.onHlsError(hls, fatal('bufferAppendError')), false);
    assert.equal(g.onHlsError(hls, { type: 'mediaError', details: 'bufferAddCodecError', fatal: false }), false);
    assert.equal(g.onHlsError(hls, { type: 'networkError', details: 'fragLoadError', fatal: true }), false);
    assert.equal(g.onHlsError(hls, null), false);
    assert.equal(g.done, false);
    assert.equal(hls.stops, 0);
    assert.deepEqual(told, []);
});

test('giveUp or stopLoad throwing: still given up', () => {
    const g = createVodGuard({ giveUp: () => { throw new Error('boom'); } });
    assert.equal(g.onHlsError({ stopLoad() { throw new Error('gone'); } }, fatal('bufferAddCodecError')), true);
    assert.equal(g.done, true);
});

// Two guards in the one slot: the first asked first, the second as it was.
test('composeGuards: the vod guard first, the audio guard as it was', () => {
    const calls = [];
    const audio = {
        setHls: (h) => calls.push(['audio.setHls', h]),
        onBufferCodecs: (d) => calls.push(['audio.codecs', d]),
        onHlsError: (h, d) => { calls.push(['audio.err', d.details]); return d.details === 'audioOnly'; },
    };
    const vod = createVodGuard({ giveUp: () => calls.push(['giveUp']) });
    const g = composeGuards(vod, audio);
    g.setHls('H');
    g.onBufferCodecs('C');
    assert.equal(g.onHlsError(fakeHls(), fatal('audioOnly')), true, 'the audio guard takes its own');
    assert.equal(g.onHlsError(fakeHls(), fatal('bufferAddCodecError')), true, 'the vod guard takes the refusal');
    assert.deepEqual(calls, [['audio.setHls', 'H'], ['audio.codecs', 'C'], ['audio.err', 'audioOnly'], ['giveUp']]);
    assert.equal(composeGuards(null, audio), audio);
    assert.equal(composeGuards(vod, null), vod);
    assert.equal(composeGuards(null, null), null);
});
