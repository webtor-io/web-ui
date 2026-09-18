import test from 'node:test';
import assert from 'node:assert/strict';
import { pickOffer, upsellSuppressed, suppressUpsell, offerVisible, offerTiming, OFFER_STORAGE_KEY } from './subtitle-offer.js';

const none = { id: 'none', lang: 'und' };
const en = { id: 'mp-0', lang: 'en', isDefault: true };
const offered = { id: 'tr-pt', lang: 'pt', offered: true, offerLabel: 'Translate to Portuguese' };
const upsell = { id: 'tr-pt', lang: 'pt', locked: true, upsell: true, offerLabel: 'Translate to Portuguese' };

function memoryStorage() {
    const m = new Map();
    return { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, String(v)) };
}

test('an offered translation is a start offer', () => {
    assert.deepEqual(pickOffer([none, en, offered]), { kind: 'start', id: 'tr-pt', lang: 'pt', label: 'Translate to Portuguese' });
});

test('a locked translation the ladder wanted is an upsell', () => {
    assert.deepEqual(pickOffer([none, en, upsell]), { kind: 'upsell', id: 'tr-pt', lang: 'pt', label: 'Translate to Portuguese' });
});

test('a locked translation nobody needs is no offer at all', () => {
    assert.equal(pickOffer([none, en, { ...upsell, upsell: false }]), null);
});

test('a translation already playing is not offered', () => {
    assert.equal(pickOffer([none, en, { ...offered, isDefault: true }]), null);
});

test('something else to turn on in that language withdraws both kinds', () => {
    const forced = { id: 'et-2', lang: 'pt' };
    assert.equal(pickOffer([none, en, forced, offered]), null);
    assert.equal(pickOffer([none, en, forced, upsell]), null);
});

test('not now lasts thirty days, not thirty-one', () => {
    const s = memoryStorage();
    suppressUpsell(s, 1000);
    assert.equal(upsellSuppressed(s, 1000 + offerTiming.snoozeMs - 1), true);
    assert.equal(upsellSuppressed(s, 1000 + offerTiming.snoozeMs + 1), false);
});

test('don’t offer is for good', () => {
    const s = memoryStorage();
    suppressUpsell(s, 1000, { never: true });
    assert.equal(upsellSuppressed(s, Number.MAX_SAFE_INTEGER), true);
});

test('storage that throws or holds junk reads as not suppressed', () => {
    const throwing = { getItem() { throw new Error('denied'); }, setItem() { throw new Error('denied'); } };
    assert.equal(upsellSuppressed(throwing, 0), false);
    assert.doesNotThrow(() => suppressUpsell(throwing, 0));
    const junk = memoryStorage();
    junk.setItem(OFFER_STORAGE_KEY, '{not json');
    assert.equal(upsellSuppressed(junk, 0), false);
    assert.equal(upsellSuppressed(undefined, 0), false);
});

test('the pill lingers, then follows the controls, and yields to catch-up', () => {
    const offer = { kind: 'start' };
    assert.equal(offerVisible({ offer, lingering: true, controlsVisible: false }), true);
    assert.equal(offerVisible({ offer, lingering: false, controlsVisible: false }), false);
    assert.equal(offerVisible({ offer, lingering: false, controlsVisible: true }), true);
    assert.equal(offerVisible({ offer, lingering: false, controlsVisible: false, cardOpen: true }), true);
    assert.equal(offerVisible({ offer, lingering: true, controlsVisible: true, catchUp: {} }), false);
    assert.equal(offerVisible({ offer: null, lingering: true, controlsVisible: true }), false);
});
