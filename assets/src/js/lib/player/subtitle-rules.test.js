import { test } from 'node:test';
import assert from 'node:assert/strict';
import { baseLang, pickDefaultSubtitle, translationAction, hasSavedDefault } from './subtitle-rules.js';

// Tracks carry the rank the server rendered as data-rank (see
// ladderRank in handlers/action/helper.go): 0 user upload, 1 embedded,
// 2 sidecar, 3 OpenSubtitles by hash, 4 OpenSubtitles by imdb id,
// 5 AI translation, 9 unknown. The client never re-derives it.
const T = (id, rank, srclang, extra = {}) => ({
    id, rank, srclang, provider: '', source: '', badge: '',
    forced: false, locked: false, isDefault: false, ...extra,
});

test('baseLang reduces regional tags', () => {
    // The server canonizes 3-letter codes to 2-letter tags before
    // rendering, so there is no ISO-639-2 map on the client.
    assert.equal(baseLang('pt-BR'), 'pt');
    assert.equal(baseLang('pt_BR'), 'pt');
    assert.equal(baseLang('EN'), 'en');
    assert.equal(baseLang(''), '');
    assert.equal(baseLang(null), '');
});

test('audio in the preferred language: forced wins, else none', () => {
    const tracks = [T('mp-0', 1, 'pt', { forced: true }), T('mp-1', 1, 'pt'), T('tr-pt', 5, 'pt', { provider: 'Translated' })];
    assert.equal(pickDefaultSubtitle(tracks, 'pt', 'pt'), 'mp-0');
    assert.equal(pickDefaultSubtitle([T('mp-1', 1, 'pt')], 'pt', 'pt'), 'none');
});

test('audio differs: ladder user > embedded > sidecar > os hash > os imdb', () => {
    const all = [
        T('tr-pt', 5, 'pt', { provider: 'Translated' }),
        T('os-1', 4, 'pt'),
        T('os-2', 3, 'pt'),
        T('et-1', 2, 'pt'),
        T('mp-0', 1, 'pt'),
        T('us-1', 0, 'pt'),
    ];
    assert.equal(pickDefaultSubtitle(all, 'en', 'pt'), 'us-1');
    assert.equal(pickDefaultSubtitle(all.slice(0, 5), 'en', 'pt'), 'mp-0');
    assert.equal(pickDefaultSubtitle(all.slice(0, 4), 'en', 'pt'), 'et-1');
    assert.equal(pickDefaultSubtitle(all.slice(0, 3), 'en', 'pt'), 'os-2');
    assert.equal(pickDefaultSubtitle(all.slice(0, 2), 'en', 'pt'), 'os-1');
    // ...and the ladder stops there. A translation is never picked by a
    // rule (owner, 2026-09-16): starting one spends tokens, so it takes a
    // click on its chip. With nothing else in the language the answer is
    // "no subtitles", and the picker offers the translation instead.
    assert.equal(pickDefaultSubtitle(all.slice(0, 1), 'en', 'pt'), 'none');
});

// Negative control for the same guard, from the other side: the rule still
// picks a human track of the WORST rank over a translation.
test('a translation never beats a human track, however far down the ladder', () => {
    const tracks = [T('tr-pt', 5, 'pt', { provider: 'Translated' }), T('x-1', 9, 'pt')];
    assert.equal(pickDefaultSubtitle(tracks, 'en', 'pt'), 'x-1');
});

test('forced never counts as a full track and a locked track is never the pick', () => {
    // A locked AI item cannot be turned on, so selecting it would leave
    // the viewer with subtitles "on" and nothing on screen.
    const tracks = [T('et-1', 2, 'pt', { forced: true }), T('tr-pt', 5, 'pt', { locked: true, provider: 'Translated' })];
    assert.equal(pickDefaultSubtitle(tracks, 'en', 'pt'), 'none');
    assert.equal(pickDefaultSubtitle([T('et-1', 2, 'pt', { forced: true })], 'en', 'pt'), 'none');
});

test('nothing qualifies: the default the server already chose stands', () => {
    // Falling through to "none" would take subtitles away from a viewer
    // who had them before touching the audio menu.
    const tracks = [T('tr-pt', 5, 'pt', { locked: true, provider: 'Translated' }), T('os-de', 3, 'de', { isDefault: true })];
    assert.equal(pickDefaultSubtitle(tracks, 'en', 'pt'), 'os-de');
    assert.equal(pickDefaultSubtitle([T('os-de', 3, 'de')], 'en', 'pt'), 'none');
});

test('unknown audio language means subtitles are needed', () => {
    assert.equal(pickDefaultSubtitle([T('os-1', 3, 'pt')], '', 'pt'), 'os-1');
});

test('no preferred language: the audio menu leaves the selection alone', () => {
    // data-preferred-lang is empty in two live configurations -- every
    // embed, and any deployment with SUBTITLE_TRANSLATE_ENABLED off -- so
    // this is not an exotic input. Answering 'none' here jumped over the
    // "keep what the server chose" fallback and turned a first-time
    // viewer's subtitles off the moment they touched the audio menu.
    const tracks = [T('os-1', 3, 'pt', { isDefault: true }), T('os-2', 3, 'en')];
    assert.equal(pickDefaultSubtitle(tracks, 'en', ''), 'os-1');
    // Nothing was on, so nothing comes on: "keep the selection" is not
    // "turn something on".
    assert.equal(pickDefaultSubtitle([T('os-1', 3, 'pt')], 'en', ''), 'none');
    // Not even a forced track in the audio's own language -- with no
    // preferred language there is no rule to apply, only a state to hold.
    assert.equal(pickDefaultSubtitle([T('mp-0', 1, 'en', { forced: true })], 'en', ''), 'none');
});

test('the none entry is never a candidate on its own merits', () => {
    assert.equal(pickDefaultSubtitle([{ id: 'none', rank: 9, srclang: '', forced: false, locked: false }], 'en', 'pt'), 'none');
});

test('translationAction: start once, resume an interrupted run, never a finished one', () => {
    const tr = { id: 'tr-ru', provider: 'Translated', locked: false };
    const status = new Map();
    assert.equal(translationAction(tr, status), 'start');

    // The run is under way; the viewer selects another track (which stops
    // the poll) and comes back. Polling must pick up again, silently.
    status.set('tr-ru', 'running');
    assert.equal(translationAction(tr, status), 'resume');

    // Finished: re-selecting it neither polls nor re-reports.
    status.set('tr-ru', 'done');
    assert.equal(translationAction(tr, status), 'none');

    // Stopped by the service (source_gone / too_large) is terminal for
    // the same reason: another poll gets the same answer and reports it
    // again. The chip says "reload to retry", and a reload is a fresh
    // status map.
    status.set('tr-ru', 'stopped');
    assert.equal(translationAction(tr, status), 'none');

    // Another language is its own translation.
    assert.equal(translationAction({ id: 'tr-de', provider: 'Translated', locked: false }, status), 'start');
});

test('translationAction: nothing to do for anything that is not a runnable AI item', () => {
    const status = new Map();
    assert.equal(translationAction({ id: 'os-1', provider: 'OpenSubtitles', locked: false }, status), 'none');
    assert.equal(translationAction({ id: 'tr-ru', provider: 'Translated', locked: true }, status), 'none');
    assert.equal(translationAction({ id: '', provider: 'Translated', locked: false }, status), 'none');
    assert.equal(translationAction(null, status), 'none');
    // A missing status map is the same as an empty one.
    assert.equal(translationAction({ id: 'tr-ru', provider: 'Translated', locked: false }, null), 'start');
});

test('hasSavedDefault only counts a default the viewer saved themselves', () => {
    assert.equal(hasSavedDefault([T('os-1', 3, 'pt', { isDefault: true, saved: true })]), true);
    // A ladder pick is not a choice: the audio rule may override it.
    assert.equal(hasSavedDefault([T('os-1', 3, 'pt', { isDefault: true })]), false);
    // Saved, but the ladder moved on (e.g. the saved item is now locked).
    assert.equal(hasSavedDefault([T('os-1', 3, 'pt', { saved: true })]), false);
    assert.equal(hasSavedDefault([]), false);
    assert.equal(hasSavedDefault(null), false);
});

test('an audio switch keeps a user-uploaded default when nothing in the preferred language qualifies', () => {
    // The "My Subtitles" list renders from its own view model. Until
    // UserSubtitleTrack carried Default/Saved those rows had no
    // data-default at all, so this fallback saw no current default and
    // switched the audio track straight to 'none' — the viewer's own
    // upload disappeared the moment they touched the audio menu.
    const tracks = [
        T('us-1', 0, 'de', { provider: 'UserSubtitle', badge: 'user', isDefault: true }),
        T('os-1', 3, 'fr'),
    ];
    assert.equal(pickDefaultSubtitle(tracks, 'en', 'pt'), 'us-1');
    // And a saved upload is a manual choice: the audio rule leaves it alone.
    assert.equal(hasSavedDefault([T('us-1', 0, 'de', { provider: 'UserSubtitle', isDefault: true, saved: true })]), true);
});

test('a user upload in the preferred language outranks every other source on an audio switch', () => {
    const tracks = [
        T('us-1', 0, 'pt', { provider: 'UserSubtitle', badge: 'user' }),
        T('mp-0', 1, 'pt'),
        T('os-1', 3, 'pt'),
    ];
    assert.equal(pickDefaultSubtitle(tracks, 'en', 'pt'), 'us-1');
});

// ---- the English track beside a translation offer ---------------------
//
// Owner, 2026-09-28: when the viewer's language has no human track and the
// audio is not in it, the best English track plays and the translation
// stays an offer. offeredDefault on the server; this is the same rule on an
// audio switch.

const AI = (id, extra = {}) => T(id, 5, id.replace(/^tr-/, ''), { provider: 'Translated', ...extra });

// Negative control: without the offer branch in pickDefaultSubtitle every
// assertion below that names an English id answers 'none'.
test('beside a translation offer the best English track by rank comes on', () => {
    const tracks = [
        AI('tr-pt'),
        T('mp-0', 1, 'en'),
        T('os-ru', 3, 'ru'),
        T('os-en', 3, 'en'),
        // Last on the list and still the pick: the rank decides, not the
        // order the server happened to render.
        T('us-1', 0, 'en', { provider: 'UserSubtitle' }),
    ];
    assert.equal(pickDefaultSubtitle(tracks, 'ja', 'pt'), 'us-1');
    assert.equal(pickDefaultSubtitle(tracks.slice(0, 4), 'ja', 'pt'), 'mp-0');
    // Unknown audio counts as foreign, as everywhere else.
    assert.equal(pickDefaultSubtitle(tracks.slice(0, 4), '', 'pt'), 'mp-0');
    // A forced English track is signs only, never the full default.
    assert.equal(pickDefaultSubtitle([AI('tr-pt'), T('mp-0', 1, 'en', { forced: true })], 'ja', 'pt'), 'none');
});

test('the English track never outranks the rules before it', () => {
    const en = T('os-en', 3, 'en');
    // Audio in the viewer's language: nothing, or a forced track -- not English.
    assert.equal(pickDefaultSubtitle([AI('tr-pt'), en], 'pt', 'pt'), 'none');
    assert.equal(pickDefaultSubtitle([AI('tr-pt'), en, T('mp-0', 1, 'pt', { forced: true })], 'pt', 'pt'), 'mp-0');
    // A human track in the viewer's language, however low its rank.
    assert.equal(pickDefaultSubtitle([AI('tr-pt'), en, T('os-pt', 4, 'pt')], 'ja', 'pt'), 'os-pt');
    // No English track: what was on stays on (the server answers "None"
    // there; the client never turns something off it cannot replace).
    assert.equal(pickDefaultSubtitle([AI('tr-pt'), T('os-ru', 3, 'ru')], 'ja', 'pt'), 'none');
    assert.equal(pickDefaultSubtitle([AI('tr-pt'), T('mp-0', 1, 'pt', { forced: true, isDefault: true })], 'ja', 'pt'), 'mp-0');
});

// Negative control for the `locked` test in translationOnOffer: without it
// a free viewer's audio switch replaces the server's phase-1 pick (here the
// Russian track Accept-Language chose) with English.
test('no offer, no English rule: a free viewer keeps what the server chose', () => {
    const tracks = [AI('tr-pt', { locked: true }), T('os-ru', 3, 'ru', { isDefault: true }), T('os-en', 3, 'en')];
    assert.equal(pickDefaultSubtitle(tracks, 'ja', 'pt'), 'os-ru');
    // ...and with nothing on, nothing comes on.
    assert.equal(pickDefaultSubtitle([AI('tr-pt', { locked: true }), T('os-en', 3, 'en')], 'ja', 'pt'), 'none');
    // No AI item at all (NSFW, a language the service does not know): the same.
    assert.equal(pickDefaultSubtitle([T('os-en', 3, 'en')], 'ja', 'pt'), 'none');
});

// Negative control: without the langGuessed filter the untagged embedded
// track (rank 1) wins over the declared English one. Same rule as
// TestOfferEnglishIsADeclaredLanguage on the server.
test('beside an offer an embedded track labelled English for lack of a tag does not come on', () => {
    const tracks = [AI('tr-pt'), T('mp-0', 1, 'en', { langGuessed: true }), T('os-en', 3, 'en')];
    assert.equal(pickDefaultSubtitle(tracks, 'ja', 'pt'), 'os-en');
    assert.equal(pickDefaultSubtitle(tracks.slice(0, 2), 'ja', 'pt'), 'none');
});
