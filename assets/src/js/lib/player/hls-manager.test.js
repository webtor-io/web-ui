import { test } from 'node:test';
import assert from 'node:assert/strict';
import { remapTrackGroup } from './hls-manager.js';

// Fake DOM element: records every setAttribute so a test can tell an
// untouched element from one that was re-pointed to the same value.
function makeEl({ mpId = '', srclang = '', label = '', dataLabel = null } = {}) {
    const attrs = { 'data-mp-id': mpId, 'data-srclang': srclang };
    if (dataLabel !== null) attrs['data-label'] = dataLabel;
    const writes = [];
    return {
        textContent: label,
        getAttribute: (n) => (n in attrs ? attrs[n] : null),
        setAttribute: (n, v) => {
            attrs[n] = v;
            writes.push([n, v]);
        },
        get mpId() {
            return attrs['data-mp-id'];
        },
        writes,
    };
}

// Since GetSubtitles started hiding bitmap/forced embedded tracks, a
// hidden track still occupies an HLS index, so elements.length !==
// hlsTracks.length is normal and the server-rendered data-mp-id is
// authoritative. A lang-only fallback would hand the visible element
// the hidden forced track's index — the exact bug this guards.
test('counts differ: a weak lang-only match must not steal the hidden track index', () => {
    const el = makeEl({ mpId: '1', srclang: 'en', label: 'Subtitle #2' });
    const hlsTracks = [
        { lang: 'en', name: 'Forced' },
        { lang: 'en', name: 'English' },
    ];
    remapTrackGroup([el], hlsTracks);
    assert.equal(el.mpId, '1');
    assert.deepEqual(el.writes, []);
});

test('counts differ: a name-only match must not override the server id either', () => {
    const el = makeEl({ mpId: '2', srclang: 'ru', label: 'English' });
    const hlsTracks = [
        { lang: 'en', name: 'Forced' },
        { lang: 'en', name: 'English' },
        { lang: 'ru', name: '' },
    ];
    remapTrackGroup([el], hlsTracks);
    assert.equal(el.mpId, '2');
    assert.deepEqual(el.writes, []);
});

test('counts differ: the exact lang+name pass still runs', () => {
    const el = makeEl({ mpId: '0', srclang: 'en', label: 'English' });
    const hlsTracks = [
        { lang: 'en', name: 'Forced' },
        { lang: 'en', name: 'English' },
    ];
    remapTrackGroup([el], hlsTracks);
    assert.equal(el.mpId, '1');
});

test('counts equal: sequential assignment is unchanged', () => {
    const els = [
        makeEl({ mpId: '', srclang: 'en', label: 'English' }),
        makeEl({ mpId: '', srclang: 'ru', label: 'Russian' }),
    ];
    remapTrackGroup(els, [
        { lang: 'ru', name: 'Russian' },
        { lang: 'en', name: 'English' },
    ]);
    assert.deepEqual(els.map((e) => e.mpId), ['0', '1']);
});

test('no elements or no tracks is a no-op', () => {
    const el = makeEl({ mpId: '3', srclang: 'en', label: 'English' });
    remapTrackGroup([el], []);
    remapTrackGroup([], [{ lang: 'en', name: 'English' }]);
    assert.equal(el.mpId, '3');
    assert.deepEqual(el.writes, []);
});

// The picker chip's text is no longer the track name: it carries the
// origin code, any property tag and the source suffix around the label.
// data-label (controller ruling R7) is what still equals the manifest's
// track name, and the exact lang+name pass is the only thing that may
// refine a server-rendered id — reading textContent here would make that
// pass dead code on every chip.
test('counts differ: the exact match reads data-label, not the chip\'s decorated text', () => {
    const el = makeEl({
        mpId: '0',
        srclang: 'en',
        dataLabel: 'English',
        label: 'EM English forced \u00b7 hash',
    });
    remapTrackGroup([el], [
        { lang: 'en', name: 'Forced' },
        { lang: 'en', name: 'English' },
    ]);
    assert.equal(el.mpId, '1');
});
