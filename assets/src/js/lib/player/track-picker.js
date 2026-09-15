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
//   #subtitle-tracks   role=radiogroup, every subtitle chip, flat.
//                      .subtitle[data-id] — including the "None" item
//                      (data-id="none"), which is not a chip at all any
//                      more but a hidden state carrier at the head of the
//                      row: the player activates it by id, the viewer
//                      switches it through #subtitles-toggle, and the
//                      filter never unhides it.
//   #subtitles-toggle  the on/off switch of the whole subtitle block.
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
// is drawn: the language playing, then the viewer's preferred one, then by
// how many tracks it has, then by the order the server rendered.
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
        if (a.active !== b.active) return a.active ? -1 : 1;
        const pa = a.lang === pref, pb = b.lang === pref;
        if (pa !== pb) return pa ? -1 : 1;
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

// expandedLangFor answers which language's tracks the row shows. The active
// one wins; otherwise the viewer's own last choice, as long as it still has
// tracks (deleting the last upload of a language must not leave an empty
// list); otherwise the preferred language; otherwise the first group.
export function expandedLangFor(chips, { preferred = '', current = '' } = {}) {
    const groups = groupByLang(chips, preferred);
    if (!groups.length) return '';
    const active = activeLang(chips);
    if (active) return active;
    const has = (l) => l && groups.some((g) => g.lang === l);
    const cur = current ? baseLang(current) : '';
    if (has(cur)) return cur;
    const pref = preferred ? baseLang(preferred) : '';
    if (has(pref)) return pref;
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
// the switch has no track to give and must not claim otherwise.
export function toggleDecision({ on, lastId = '', suggestedId = '', tracks = [], audioLang = '', preferredLang = '' } = {}) {
    if (!on) return { activateId: 'none', persist: true };
    const list = Array.isArray(tracks) ? tracks : [];
    const usable = (id) => !!id && id !== 'none' && list.some((t) => t && t.id === id && !t.locked);
    if (usable(lastId)) return { activateId: lastId, persist: true };
    if (usable(suggestedId)) return { activateId: suggestedId, persist: true };
    const id = pickDefaultSubtitle(list, audioLang, preferredLang);
    if (!usable(id)) return { activateId: '', persist: false };
    return { activateId: id, persist: true };
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
        el,
    };
}

// readChips reads the flat subtitle list. The "Off" chip is NOT in it: it
// lives in the language row (#subtitle-langs) as the switch next to the
// languages, so the filter never sees it and never hides it; the callers
// that would have to skip it by id keep doing so defensively. Uploads land
// in #my-subtitles, a display:contents wrapper inside the same container, so
// they are read here too.
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
        if (c.id !== 'none' && attr(c.el, 'data-suggested') === 'true') return c.id;
    }
    return '';
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
// clicking it opens the upgrade CTA instead of switching the track.
export function setChipActive(el, on) {
    if (!el || !el.classList) return;
    const active = !!on && attr(el, 'data-locked') !== 'true';
    el.classList.toggle(ACTIVE_TRACK_CLASS, active);
    if (el.setAttribute) el.setAttribute('aria-checked', active ? 'true' : 'false');
    const check = el.querySelector && el.querySelector('.chip-check');
    if (check) check.hidden = !active;
}

function setLangChipActive(el, on) {
    if (!el) return;
    if (el.classList) el.classList.toggle(ACTIVE_LANG_CLASS, !!on);
    if (el.setAttribute) el.setAttribute('aria-pressed', on ? 'true' : 'false');
}

// applyLangFilter shows the tracks of one language and hides the rest. The
// "None" item is hidden in every language: it is no longer a chip the
// viewer presses (the toggle on the heading is), only the element the
// player activates by id, and revealing it would put a nameless button in
// the row.
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
            row.insertBefore(el, more || null);
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
    for (const sel of ['#subtitle-langs', '#subtitle-tracks']) {
        const box = container.querySelector(sel);
        if (box && box.classList) box.classList.toggle('picker-off', next);
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

// refreshMarks is what a selection needs: the row's counts and dot, and the
// summaries. It deliberately does NOT re-run the language filter — the
// viewer's expanded language is their choice and must not jump under them.
export function refreshMarks(container) {
    syncLangRow(container);
    syncNow(container);
}

// refresh is the full pass (R9: row, then filter, then summary): used when
// the dialog opens and after the uploads partial is swapped in, where the
// set of chips itself changed. Returns the language it settled on.
export function refresh(container, { current = '' } = {}) {
    if (!container) return '';
    const preferred = attr(container, 'data-preferred-lang');
    const lang = expandedLangFor(readChips(container), {
        preferred,
        current: current || expandedLang(container),
    });
    syncLangRow(container, lang);
    applyLangFilter(container, lang);
    applyFlagSupport(container);
    applyOffState(container);
    syncNow(container);
    return lang;
}
