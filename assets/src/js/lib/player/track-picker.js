// The track picker (docs/uikit.html §19) is server-rendered: Go emits every
// audio and subtitle chip into one flat container per group, plus the
// subtitle language row. This module is the progressive-enhancement half —
// it filters the flat list by language, keeps the row's counts and the dot
// on the active language honest, and writes the "Now:" summary. Without it
// the dialog still shows the tracks of the expanded language (the server
// renders `hidden` on the rest) and every chip is still clickable.
//
// The pure half (groupByLang / activeLang / expandedLangFor / langRowOps)
// takes plain objects so `node --test` can exercise it, and mirrors
// handlers/action.Helper.SubtitleLangGroups exactly: Go renders the first
// row, this recomputes it after an upload adds or removes a track, and a
// disagreement between the two would make the row jump on the first click.
//
// DOM contract (controller rulings R1-R9 on the track-picker plan):
//
//   #subtitle-tracks   role=radiogroup, every subtitle chip, flat —
//                      uploads included, moved in by adoptUploadChips when
//                      an async reload of the uploads partial delivers them
//                      into #my-subtitles (the wrapper below the row, which
//                      otherwise holds only the uploads disclosure and its
//                      panel: a radiogroup contains radios and nothing
//                      else).
//                      .subtitle[data-id] — including the "None" item
//                      (data-id="none"), which is not a chip at all any
//                      more but a hidden state carrier at the head of the
//                      row: the player activates it by id, the viewer
//                      switches it through #subtitles-toggle, and the
//                      filter never unhides it.
//   #subtitle-hint     explains a pending offer (a translation marked
//                      data-offered, not the track playing, in a language
//                      with nothing else the viewer could turn on).
//   #subtitles-toggle  the on/off switch of the whole subtitle block, the
//                      first child of #subtitle-langs (the chips live in
//                      .lang-row beside it, which is what the muted state
//                      dims — the switch itself never fades).
//                      Off is mirrored on the dialog as
//                      data-subtitles-off="true", the chip that comes back
//                      as data-last-subtitle (this session) or
//                      data-suggested (the server's ladder answer), and
//                      drawn as .picker-off on both rows.
//   #subtitle-langs    role=group. Language chips are
//                      <button class="lang lang-chip" data-lang aria-pressed>
//                      with .lang-name / .lang-count / .lang-dot / .chip-flag
//                      inside. #subtitle-lang-more (with .more-count and
//                      aria-expanded) collapses the tail; #lang-chip-template
//                      is the <template> a new language is cloned from.
//   #audio-tracks      the same, for .audio[data-label] chips.
//   #audio-now /       .now-origin (subtitles only) + .now-value.
//   #subtitle-now
//
// Active state is ONE class (R6): the colours live in style.css behind
// .track-chip-active / .lang-chip-active, so no Tailwind utility is ever
// spelled out here.

import { supportsFlagEmoji } from '../discover/lang.js';
import { pickDefaultSubtitle } from './subtitle-rules.js';

// Mirrors maxVisibleLangChips in handlers/action/picker.go. Changing one
// without the other makes the row jump between the server's first paint and
// the client's first refresh.
export const MAX_VISIBLE_LANGS = 6;

const ACTIVE_TRACK_CLASS = 'track-chip-active';
const ACTIVE_LANG_CLASS = 'lang-chip-active';
// The "+N" button becomes the collapse control once the row is open. A
// glyph, not a word: the button's aria-label already carries the localized
// name and this module has no copy of any string.
const COLLAPSE_GLYPH = '×';

// baseLang is the grouping key: "pt-BR" and "pt_br" are the same chip, an
// untagged track is "und". Mirrors stremio.NewLangDisplay's Lang.
export function baseLang(v) {
    const s = String(v == null ? '' : v).trim().toLowerCase();
    if (!s) return 'und';
    return s.split(/[-_]/)[0] || 'und';
}

// groupByLang collapses chips into language groups, ordered the way the row
// is drawn: the viewer's preferred language, then the one playing, then by
// how many tracks it has, then by the order the server rendered.
//
// Preferred ahead of playing is the owner's call (2026-09-15): the row is
// where the viewer looks for their own language, and what is playing keeps
// the dot on its chip wherever that chip sorts — possibly hidden under the
// filter, which is accepted. Mirrors handlers/action.SubtitleLangGroups.
//
// That last tie-break is the order of the input, kept by a stable sort —
// NOT the language name. Go's SubtitleLangGroups ends its comparator with
// `return false` for the same reason: the chips arrive in relevance order
// from GetSubtitles, and re-sorting equal-count groups alphabetically on
// the client would reshuffle the row on the first refresh after an upload.
export function groupByLang(chips, preferred = '') {
    // An unset preference must not resolve to "und" and beat the genuine
    // Unknown-language group (Go: 8ae6274).
    const pref = preferred ? baseLang(preferred) : '';
    const order = [];
    const byLang = new Map();
    for (const c of chips) {
        if (!c || c.id === 'none') continue;
        const lang = baseLang(c.lang);
        let g = byLang.get(lang);
        if (!g) {
            g = { lang, count: 0, active: false };
            byLang.set(lang, g);
            order.push(g);
        }
        g.count++;
        if (c.isDefault) g.active = true;
    }
    return order.slice().sort((a, b) => {
        const pa = a.lang === pref, pb = b.lang === pref;
        if (pa !== pb) return pa ? -1 : 1;
        if (a.active !== b.active) return a.active ? -1 : 1;
        return b.count - a.count;
    });
}

// activeLang is the language of the track playing, or '' when subtitles are
// off. "Off" is a choice, not a language, so it has no group.
export function activeLang(chips) {
    for (const c of chips) {
        if (c && c.isDefault && c.id !== 'none') return baseLang(c.lang);
    }
    return '';
}

// expandedLangFor answers which language's tracks the row shows. The
// viewer's own last choice wins, as long as it still has tracks (deleting
// the last upload of a language must not leave an empty list); otherwise
// the first group — which groupByLang has already made the preferred
// language, then the one playing, then the largest.
//
// `current` ahead of everything is what keeps a refresh (an upload, a
// delete, reopening the dialog) from yanking the viewer out of the
// language they were browsing. On a first open `current` is the chip the
// server pressed, i.e. LangRow.Expanded — so the two sides agree without
// either recomputing the other's answer.
export function expandedLangFor(chips, { preferred = '', current = '' } = {}) {
    const groups = groupByLang(chips, preferred);
    if (!groups.length) return '';
    const has = (l) => l && groups.some((g) => g.lang === l);
    const cur = current ? baseLang(current) : '';
    if (has(cur)) return cur;
    return groups[0].lang;
}

// langRowOps is what the row has to become: one update per chip already in
// it, the chips a language has but the row does not (an upload in a new
// language), and how many chips end up behind "+N".
//
// A chip whose language lost its last track gets count 0 and hidden — it is
// gone, not collapsed, so it is not counted into the overflow either: "+1"
// next to a chip nothing can bring back would be a lie.
//
// The expanded language is never cut by the overflow, wherever it sorts: the
// viewer reached it by opening "+N" and pressing it, and collapsing the row
// again would hide the very chip whose tracks are on screen — leaving a
// pressed-but-invisible filter and no way back to it short of re-expanding.
// (An emptied language is still hidden: that is "gone", not "collapsed".)
export function langRowOps(chips, rowChips, expanded, preferred = '') {
    const groups = groupByLang(chips, preferred);
    const pos = new Map(groups.map((g, i) => [g.lang, i]));
    const byLang = new Map(groups.map((g) => [g.lang, g]));
    const exp = expanded ? baseLang(expanded) : '';

    const entry = (lang) => {
        const g = byLang.get(lang);
        const i = pos.has(lang) ? pos.get(lang) : Number.MAX_SAFE_INTEGER;
        return {
            lang,
            count: g ? g.count : 0,
            active: !!(g && g.active),
            selected: lang === exp,
            hidden: !g || (i >= MAX_VISIBLE_LANGS && lang !== exp),
        };
    };

    const updates = rowChips.map((rc) => entry(baseLang(rc.lang)));
    const known = new Set(updates.map((u) => u.lang));
    const missing = [];
    for (const g of groups) {
        if (known.has(g.lang)) continue;
        const src = chips.find((c) => c && c.id !== 'none' && baseLang(c.lang) === g.lang) || {};
        missing.push({ ...entry(g.lang), name: src.langName || '', flag: src.langFlag || '' });
    }
    const overflow = updates.concat(missing).filter((u) => u.count > 0 && u.hidden).length;
    return { updates, missing, overflow };
}

// toggleDecision is the whole rule behind the subtitles switch: what to
// activate, and whether the result is the viewer's own choice.
//
// Off is always one answer -- the "None" item -- because "off" is a state
// the viewer asked for and the next page load must reproduce it. On has
// three candidates in order: what was playing before it was switched off
// this session (lastId), what the server said it would have picked
// (suggestedId, ListItem.Suggested), and finally the ladder rule itself.
//
// A candidate counts only while it is still in the list and not locked: a
// deleted upload or an AI track a free viewer cannot open would leave
// subtitles "on" with nothing on screen. When nothing at all is
// activatable the answer is to activate nothing and persist nothing --
// the switch has no track to give and must not claim otherwise, so the
// caller puts it back where it was.
//
// A translation is a candidate for lastId ONLY (owner, 2026-09-16):
// starting one spends tokens, so the switch never picks one up on the
// viewer's behalf -- not from the server's offer, not from the ladder
// rule. Through lastId it is free: the viewer ran that very translation
// earlier in this session and it is cached.
export function toggleDecision({ on, lastId = '', suggestedId = '', tracks = [], audioLang = '', preferredLang = '' } = {}) {
    if (!on) return { activateId: 'none', persist: true };
    const list = Array.isArray(tracks) ? tracks : [];
    const usable = (id, allowAI) => !!id && id !== 'none' && list.some((t) =>
        t && t.id === id && !t.locked && (allowAI || t.provider !== 'Translated'));
    if (usable(lastId, true)) return { activateId: lastId, persist: true };
    if (usable(suggestedId, false)) return { activateId: suggestedId, persist: true };
    const id = pickDefaultSubtitle(list, audioLang, preferredLang);
    if (!usable(id, false)) return { activateId: '', persist: false };
    return { activateId: id, persist: true };
}

// offerNeedsHint is the offer #subtitle-hint may speak for: a translation
// marked Offered, not the track already playing, in a language that has
// nothing else the viewer could turn on. All three, because the sentence
// is "no subtitles in <language> yet — turn on the AI translation", and
// one forced sidecar in that language makes the first half false however
// pending the offer is.
//
// "Something else" means a chip of the same language that is activatable:
// not this offer, not the hidden "None" carrier, not a locked item. The
// offer's own language is the test, not the viewer's preference setting --
// the ladder only ever builds a translation in the preferred language, so
// the two are the same set and this one cannot drift from the chip it is
// about.
//
// An offer that has been taken is no longer pending on either count -- the
// chip becomes the default, and setChipActive drops data-offered -- so the
// hint goes as soon as the run starts and does not come back.
export function offerNeedsHint(chips) {
    const list = Array.isArray(chips) ? chips : [];
    for (const c of list) {
        if (!c || !c.offered || c.isDefault) continue;
        const rival = list.some((o) => o && o !== c && o.id && o.id !== 'none' && !o.locked && o.lang === c.lang);
        if (!rival) return c.id;
    }
    return null;
}

// offStateAfterActivate is the switch's state after an activation, whoever
// asked for it: the viewer flipping the switch, the viewer pressing a chip,
// or the player itself (a deleted upload landing on "None", the
// audio-switch re-pick, an upload selected right after it was added).
//
// The invariant is one sentence — subtitles are off exactly when the
// "None" item is the active one — and it has exactly one writer
// (markTrack in Player.jsx) so that no activation can leave the switch
// saying one thing and the track row another.
//
// What comes back is recorded only on the way out: activating "None"
// remembers the track it replaced, and remembers nothing when there was
// nothing playing (the chip was deleted with its file, or the page opened
// off) — an older memory is dropped rather than kept, because a
// data-last-subtitle naming a chip that is gone would resurrect it. Turning
// subtitles on leaves the memory alone: it is overwritten the next time
// they go off.
export function offStateAfterActivate(prevDefaultId, newId, lastId = '') {
    if (newId !== 'none') return { off: false, lastId };
    const prev = prevDefaultId && prevDefaultId !== 'none' ? prevDefaultId : '';
    return { off: true, lastId: prev };
}

// restoreSavedTranslation answers "did this page load with a translation
// the viewer had chosen, and does it need starting?" -- and nothing else.
//
// The server renders such an item Default+Saved, but a translation is not a
// <track> in the page (markPreload skips it: preloading would start a run
// for everyone who opens the page), so unlike every other saved choice it
// does not resume by itself. Until 2026-09-16 the engagement gate happened
// to start it; that gate is gone, and this is the one automatic start that
// survives it -- the run is the viewer's own, already paid for and cached,
// so bringing it back costs nothing.
//
// Saved AND playing AND unlocked, all three: the ladder's own pick is not
// the viewer's choice, a saved id that is not the default is some other
// page's state, and a locked item cannot run at all.
export function restoreSavedTranslation(tracks) {
    for (const t of Array.isArray(tracks) ? tracks : []) {
        if (t && t.id && t.provider === 'Translated' && t.isDefault && t.saved && !t.locked) return t.id;
    }
    return null;
}

// ---- DOM half -------------------------------------------------------

function attr(el, name) {
    if (!el || !el.getAttribute) return '';
    return el.getAttribute(name) || '';
}

function chipData(el) {
    return {
        id: attr(el, 'data-id'),
        lang: baseLang(attr(el, 'data-lang')),
        langName: attr(el, 'data-lang-name'),
        langFlag: attr(el, 'data-lang-flag'),
        isDefault: attr(el, 'data-default') === 'true',
        locked: attr(el, 'data-locked') === 'true',
        offered: attr(el, 'data-offered') === 'true',
        el,
    };
}

// UPLOAD_PROVIDER is what a MY chip is, wherever it sits. The chips live in
// the radiogroup with every other track; the provider is the only thing
// that marks them as the set the uploads partial owns.
const UPLOAD_PROVIDER = 'UserSubtitle';

// adoptUploadChips reconciles the MY chips of #subtitle-tracks with the set
// an async reload of the uploads partial just delivered, and returns the
// chips it moved in.
//
// Why anything moves. #my-subtitles sits AFTER the radiogroup now: the
// uploads partial emits a disclosure button and a panel holding two kinds
// of form, and a role="radiogroup" contains radios and nothing else. The
// chips it also emits ARE radios and belong in the row.
//
// What decides. The partial wraps those chips in #my-upload-chips, and that
// element is rendered on the async reload only. Its presence says "this is
// the server's complete current list of this viewer's uploads", which is
// the one thing the DOM cannot otherwise tell:
//
//   absent   — an ordinary page: the dialog's own loop rendered the uploads
//              into the row, nothing was re-sent, and this is a no-op. That
//              is what keeps a page whose JS never ran correct.
//   present  — an upload or a delete: every MY chip in the row is replaced
//              by what came back. An upload's chip is therefore the fresh
//              element (carrying data-autoselect, and never the stale
//              data-default the async response deliberately omits), and a
//              deleted one is gone rather than orphaned in a row nothing
//              re-renders.
//
// The wrapper is removed once emptied, so the next refresh sees "absent"
// again and does not read an already-consumed answer as "the viewer has no
// uploads".
export function adoptUploadChips(container) {
    if (!container || !container.querySelector) return [];
    const box = container.querySelector('#subtitle-tracks');
    const src = container.querySelector('#my-upload-chips');
    if (!box || !src || !src.querySelectorAll) return [];
    const incoming = Array.from(src.querySelectorAll('.subtitle[data-id]'));
    for (const el of box.querySelectorAll('.subtitle[data-id]')) {
        if (attr(el, 'data-provider') === UPLOAD_PROVIDER && el.remove) el.remove();
    }
    for (const el of incoming) box.append(el);
    if (src.remove) src.remove();
    return incoming;
}

// readChips reads the flat subtitle list. The "Off" chip is NOT in it: it
// lives in the language row (#subtitle-langs) as the switch next to the
// languages, so the filter never sees it and never hides it; the callers
// that would have to skip it by id keep doing so defensively. Uploads are
// in the row like every other track (adoptUploadChips puts the ones an
// async reload delivers there), so they are read here too.
export function readChips(container) {
    const box = container && container.querySelector && container.querySelector('#subtitle-tracks');
    if (!box) return [];
    const chips = Array.from(box.querySelectorAll('.subtitle[data-id]')).map(chipData);
    const muted = mutedChoiceID(container, chips);
    if (!muted) return chips;
    // With subtitles off, data-default sits on the "None" carrier and the
    // row would read as "no language playing": no dot, and the filter free
    // to jump elsewhere on the next refresh. The muted choice is what the
    // switch gives back, so for the row's purposes it is the active one.
    for (const c of chips) {
        if (c.id === muted) c.isDefault = true;
    }
    return chips;
}

// mutedChoiceID is the track the switch would restore, or '' when
// subtitles are on. Within a session the player remembers the last choice
// on the dialog (data-last-subtitle); on a page opened with subtitles off
// there is no such memory and the server's suggestion stands in.
function mutedChoiceID(container, chips) {
    if (attr(container, 'data-subtitles-off') !== 'true') return '';
    const last = attr(container, 'data-last-subtitle');
    if (last) return last;
    for (const c of chips) {
        // data-suggested is only ever a track the switch can restore: the
        // translation on offer carries data-offered instead (two questions,
        // two fields), so no provider test is needed here.
        if (c.id !== 'none' && attr(c.el, 'data-suggested') === 'true') return c.id;
    }
    return '';
}

// langRowBox is where the language chips live: .lang-row inside
// #subtitle-langs, which also holds the switch. Older markup (and the
// tests' minimal fixtures) put the chips straight into the row, so the
// container itself is the fallback.
function langRowBox(container) {
    const row = container && container.querySelector && container.querySelector('#subtitle-langs');
    if (!row) return null;
    return row.querySelector('.lang-row') || row;
}

function langChipEls(container) {
    const row = container && container.querySelector && container.querySelector('#subtitle-langs');
    if (!row) return [];
    // #subtitle-lang-more carries no data-lang, and a <template>'s content is
    // a separate fragment, so neither can be mistaken for a real chip.
    return Array.from(row.querySelectorAll('.lang[data-lang]'));
}

// setChipActive is the one place a track chip's active look is written: the
// component class plus the check icon that was already in the markup.
// Classes, hidden and aria only — never innerHTML, so a chip keeps its
// origin badge, its property tag and its translation-progress span across
// every selection. data-default stays the caller's business (Player.jsx
// writes it next to this call and syncNow reads it back).
//
// A locked chip (the AI track on a free account) can never become active:
// clicking it opens the upgrade CTA instead of switching the track. Neither
// can the "None" carrier: it is hidden, unlabelled and aria-hidden, so the
// active look on it would be an invisible chip claiming to be the chosen
// one and an aria-checked radio in a group where nothing appears checked.
// Its data-default still moves -- that is the state the switch is read
// from -- but never the look.
export function setChipActive(el, on) {
    if (!el || !el.classList) return;
    const active = !!on && attr(el, 'data-locked') !== 'true' && attr(el, 'data-id') !== 'none';
    el.classList.toggle(ACTIVE_TRACK_CLASS, active);
    if (el.setAttribute) el.setAttribute('aria-checked', active ? 'true' : 'false');
    const check = el.querySelector && el.querySelector('.chip-check');
    if (check) check.hidden = !active;
    // An offer taken is an offer spent: the run is cached from here on and
    // comes back through data-last-subtitle, so the accent (and the hint
    // that reads this attribute) must not resurface after it.
    if (active && attr(el, 'data-offered') === 'true') {
        el.removeAttribute('data-offered');
        el.classList.remove('chip-offered');
    }
    // An offered AI chip is a verb until it is the track playing: "✦ AI
    // Translate to German" idle, "German · from IN" plus the progress once
    // it is on. Both states ship in the markup and are toggled here, so the
    // chip keeps its origin badge and its .tr-progress span across every
    // flip.
    //
    // Only a chip that HAS a verb flips. The verb spans are rendered for an
    // offered item alone, while .ai-label is on every AI chip -- so without
    // this test the clearing pass (markTrack, applyOffState) hid the label
    // of a locked chip, or of one whose offer was taken and then
    // deselected, and left an empty button behind (found in review,
    // 2026-09-16).
    if (el.querySelectorAll && el.querySelector && el.querySelector('.ai-action')) {
        for (const n of el.querySelectorAll('.ai-action')) n.hidden = active;
        for (const n of el.querySelectorAll('.ai-label')) n.hidden = !active;
    }
}

function setLangChipActive(el, on) {
    if (!el) return;
    if (el.classList) el.classList.toggle(ACTIVE_LANG_CLASS, !!on);
    if (el.setAttribute) el.setAttribute('aria-pressed', on ? 'true' : 'false');
}

// applyLangFilter shows the tracks of one language and hides the rest. The
// "None" item is hidden in every language: it is no longer a chip the
// viewer presses (the switch leading the language row is), only the
// element the player activates by id, and revealing it would put a
// nameless button in the row.
export function applyLangFilter(container, lang) {
    const want = baseLang(lang);
    for (const c of readChips(container)) {
        if (c.id === 'none') {
            c.el.hidden = true;
            continue;
        }
        c.el.hidden = c.lang !== want;
    }
    for (const el of langChipEls(container)) {
        setLangChipActive(el, baseLang(attr(el, 'data-lang')) === want);
    }
}

// expandedLang is the language the row is currently filtered to.
export function expandedLang(container) {
    for (const el of langChipEls(container)) {
        if (attr(el, 'aria-pressed') === 'true') return baseLang(attr(el, 'data-lang'));
    }
    return '';
}

function applyLangUpdate(el, u, rowExpanded) {
    const count = el.querySelector && el.querySelector('.lang-count');
    if (count) count.textContent = String(u.count);
    const dot = el.querySelector && el.querySelector('.lang-dot');
    if (dot) dot.hidden = !u.active;
    setLangChipActive(el, u.selected);
    // An empty language is gone from the row whatever the disclosure says;
    // a collapsed one comes back when the viewer opens "+N".
    el.hidden = u.count === 0 || (u.hidden && !rowExpanded);
}

// syncLangRow rewrites the counts, the dot, the overflow and — when an
// upload brought a language the server did not render a chip for — clones
// one out of <template id="lang-chip-template">. Filled through textContent:
// no HTML is built here.
export function syncLangRow(container, expanded) {
    const row = container && container.querySelector && container.querySelector('#subtitle-langs');
    if (!row) return;
    const preferred = attr(container, 'data-preferred-lang');
    const chips = readChips(container);
    const exp = expanded || expandedLang(container);
    const els = langChipEls(container);
    const ops = langRowOps(chips, els.map((el) => ({ lang: attr(el, 'data-lang') })), exp, preferred);

    const more = row.querySelector('#subtitle-lang-more');
    const rowExpanded = more ? attr(more, 'aria-expanded') === 'true' : false;

    els.forEach((el, i) => applyLangUpdate(el, ops.updates[i], rowExpanded));

    const tpl = row.querySelector('#lang-chip-template');
    const proto = tpl && tpl.content ? tpl.content.firstElementChild : null;
    if (proto) {
        for (const m of ops.missing) {
            const el = proto.cloneNode(true);
            el.setAttribute('data-lang', m.lang);
            const flag = el.querySelector('.chip-flag');
            if (flag) {
                flag.textContent = m.flag;
                flag.hidden = !m.flag || !supportsFlagEmoji();
            }
            const name = el.querySelector('.lang-name');
            if (name) name.textContent = m.name || m.lang.toUpperCase();
            applyLangUpdate(el, m, rowExpanded);
            // Into the chips' own box, next to the "+N" it must precede —
            // insertBefore throws when the reference node is not a child of
            // the node it is called on, and `more` lives in .lang-row.
            (more && more.parentNode ? more.parentNode : (langRowBox(container) || row)).insertBefore(el, more || null);
        }
    }
    if (more) {
        // Visible whenever anything is collapsible: collapsed it reads "+N",
        // expanded it is the way back. Its aria-label (the localized "More
        // languages") is written by the template and never touched here.
        more.hidden = ops.overflow === 0;
        const n = more.querySelector('.more-count');
        if (n) n.textContent = rowExpanded ? COLLAPSE_GLYPH : '+' + ops.overflow;
    }
}

// toggleLangOverflow opens the row to every language and closes it back.
// Returns the new state so the caller can log or test it.
export function toggleLangOverflow(container, on) {
    const more = container && container.querySelector && container.querySelector('#subtitle-lang-more');
    if (!more) return false;
    const next = on === undefined ? attr(more, 'aria-expanded') !== 'true' : !!on;
    more.setAttribute('aria-expanded', next ? 'true' : 'false');
    syncLangRow(container);
    return next;
}

// syncNow writes the "Now:" summary of both groups off the DOM, reusing the
// strings the server already rendered on the active chip. Nothing is
// translated here — the client has no copy of these names.
export function syncNow(container) {
    if (!container || !container.querySelector) return;
    const write = (boxSel, chipSel) => {
        const box = container.querySelector(boxSel);
        if (!box) return;
        const el = container.querySelector(chipSel);
        const origin = box.querySelector('.now-origin');
        if (origin) {
            // .chip-origin is the origin badge; .badge.font-mono is what the
            // same span looked like before it got a name of its own.
            const code = el && (el.querySelector('.chip-origin') || el.querySelector('.badge.font-mono'));
            const text = code ? String(code.textContent || '').trim() : '';
            origin.textContent = text;
            origin.hidden = !text;
        }
        const value = box.querySelector('.now-value');
        if (!value) return;
        if (!el) {
            value.textContent = '';
            return;
        }
        const flag = attr(el, 'data-lang-flag');
        const name = attr(el, 'data-lang-name');
        // "Off", and audio tracks the language name does not describe, fall
        // back to the label the server put on the chip.
        const label = attr(el, 'data-label') || String(el.textContent || '').trim();
        value.textContent = name ? ((flag && supportsFlagEmoji() ? flag + ' ' : '') + name) : label;
    };
    write('#audio-now', '.audio[data-default="true"]');
    write('#subtitle-now', '.subtitle[data-default="true"]');
}

// applyFlagSupport hides every flag when the platform renders regional
// indicators as bare letter pairs (Windows outside Firefox) — the same
// guard Discover applies to its own chips.
export function applyFlagSupport(container) {
    if (!container || !container.querySelectorAll) return;
    if (supportsFlagEmoji()) return;
    for (const el of container.querySelectorAll('.chip-flag')) el.hidden = true;
}

// applyOffState draws the switch's state: the attribute the whole picker
// reads, the checkbox itself, the muted look on both rows, and
// aria-disabled on the chips.
//
// Muted is not disabled. Every chip stays clickable while subtitles are
// off -- clicking one is how the viewer turns them back on with that very
// track (Player.jsx) -- so nothing here sets `disabled`, and the chips
// keep the classes they had, including the active mark on the choice that
// comes back. aria-disabled is the honest reading for a screen reader of
// a row whose selection is not currently playing.
//
// Called with no `off` to apply what the DOM already says (the server
// renders the state; this is what adds the parts only JS can).
export function applyOffState(container, off) {
    if (!container || !container.querySelector) return false;
    const next = off === undefined ? attr(container, 'data-subtitles-off') === 'true' : !!off;
    if (container.setAttribute) container.setAttribute('data-subtitles-off', next ? 'true' : 'false');
    const toggle = container.querySelector('#subtitles-toggle');
    if (toggle) toggle.checked = !next;
    // The chips dim, the switch does not: it is the way back.
    for (const box of [langRowBox(container), container.querySelector('#subtitle-tracks')]) {
        if (box && box.classList) box.classList.toggle('picker-off', next);
    }
    const toggleBox = container.querySelector('#subtitle-langs');
    if (toggleBox && toggleBox.classList && toggleBox !== langRowBox(container)) {
        toggleBox.classList.remove('picker-off');
    }
    const chips = readChips(container);
    // What the switch would give back keeps the active mark: activating
    // the "None" item runs markTrack, which clears every other chip on its
    // way through, and a muted block with nothing marked would not say
    // what comes back.
    const muted = next ? mutedChoiceID(container, chips) : '';
    for (const c of chips) {
        if (next) c.el.setAttribute('aria-disabled', 'true');
        // A locked chip is disabled for its own reason (no Src, supporters
        // only) and must stay so when the switch comes back on.
        else if (!c.locked) c.el.removeAttribute('aria-disabled');
        if (muted) setChipActive(c.el, c.id === muted);
    }
    return next;
}

// syncHint shows or hides the explanation of a pending offer. Called
// wherever the chips may have moved: the full refresh, and refreshMarks --
// which every activation ends in -- so an offer taken stops being
// explained the moment it is taken.
function syncHint(container) {
    const hint = container && container.querySelector && container.querySelector('#subtitle-hint');
    if (hint) hint.hidden = !offerNeedsHint(readChips(container));
}

// refreshMarks is what a selection needs: the row's counts and dot, and the
// summaries. It deliberately does NOT re-run the language filter — the
// viewer's expanded language is their choice and must not jump under them.
export function refreshMarks(container) {
    syncLangRow(container);
    syncHint(container);
    syncNow(container);
}

// refresh is the full pass (R9: row, then filter, then summary): used when
// the dialog opens and after the uploads partial is swapped in, where the
// set of chips itself changed. Returns the language it settled on.
export function refresh(container, { current = '' } = {}) {
    if (!container) return '';
    // Before anything reads the row: an async reload leaves the MY chips in
    // the wrapper below it, and every pass here (counts, filter, muted
    // state) is scoped to #subtitle-tracks. Idempotent, so the mount-time
    // call costs nothing.
    adoptUploadChips(container);
    const preferred = attr(container, 'data-preferred-lang');
    const lang = expandedLangFor(readChips(container), {
        preferred,
        current: current || expandedLang(container),
    });
    syncLangRow(container, lang);
    applyLangFilter(container, lang);
    applyFlagSupport(container);
    applyOffState(container);
    syncHint(container);
    syncNow(container);
    return lang;
}
