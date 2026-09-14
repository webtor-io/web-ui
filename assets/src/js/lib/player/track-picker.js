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
//                      .subtitle[data-id] — including the "Off" chip
//                      (data-id="none"), which is a choice, not a language:
//                      it is never grouped and never hidden by the filter.
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
            hidden: !g || i >= MAX_VISIBLE_LANGS,
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

// readChips reads the flat subtitle list, the "Off" chip included: it is the
// first .subtitle of #subtitle-tracks (R1) and the callers that must ignore
// it do so by id. Uploads land in #my-subtitles, a display:contents wrapper
// inside the same container, so they are read here too.
export function readChips(container) {
    const box = container && container.querySelector && container.querySelector('#subtitle-tracks');
    if (!box) return [];
    return Array.from(box.querySelectorAll('.subtitle[data-id]')).map(chipData);
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
// "Off" chip is never hidden (R1): it is the way back to no subtitles and
// has to stay reachable from every language.
export function applyLangFilter(container, lang) {
    const want = baseLang(lang);
    for (const c of readChips(container)) {
        if (c.id === 'none') {
            c.el.hidden = false;
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
    syncNow(container);
    return lang;
}
