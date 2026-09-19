// The subtitles/audio dialog, outside the Preact tree: the server renders it,
// and this module makes it work -- creating <track>s on demand, moving the
// active marks, persisting the viewer's choices, the uploads panel, the
// delegated listeners (wireTrackHandlers) and swapping in a freshly rendered
// dialog when the preferred language changes (swapSubtitlesDialog).
//
// Moved out of Player.jsx on 2026-09-19, unchanged: that file held the
// player component AND this, 2900 lines of it, and the two only ever meet
// through `hooks` (trackHooks) -- the component listens, this module calls.

import { applySubtitleSelection, selectionFor } from './subtitle-apply.js';
import { dropDeletedTracks } from './subtitle-track-reload.js';
import { readTracks, selectEventData } from './subtitle-telemetry.js';
import { withParam } from './subtitle-progress.js';
import { wirePreferredLang } from './preferred-lang.js';
import { rebindAsync } from '../async';
import {
    adoptUploadChips,
    refresh,
    refreshMarks,
    applyLangFilter,
    applyFlagSupport,
    applyOffState,
    offStateAfterActivate,
    toggleDecision,
    setChipActive,
    expandedLang,
    toggleLangOverflow,
} from './track-picker.js';

// Ensure a <track> with id=<trackID> exists inside <video>. The server
// pre-wraps user-subtitle URLs through /ext/ with the correct auth
// (subdomain/path/query baked in by torrent-http-proxy) and returns
// them in data-src. For subs uploaded after the initial render, the
// <video> has no matching <track> yet — create one on first click.
// ensureTrackElement creates the <track> for a side-loaded subtitle the
// first time it is selected. The server renders only the default track:
// browsers fetch every <track> element on page load regardless of mode,
// and 40+ OpenSubtitles tracks per page tripped the ingress per-IP rate
// limit and OpenSubtitles' 5 req/s (a burst of 40 requests in one second
// per viewer). Creating tracks lazily makes each selection one download.
export function ensureTrackElement(video, trackID, wrappedSrc, label, srclang, kind) {
    for (const t of video.querySelectorAll('track')) {
        if (t.id === trackID) return true;
    }
    if (!wrappedSrc) return false;
    const track = document.createElement('track');
    track.id = trackID;
    track.kind = kind || 'subtitles';
    track.src = wrappedSrc;
    track.label = label || 'Subtitle';
    // HTML requires srclang on a subtitles track; 'und' when the upload
    // declares no language. An empty attribute is not a valid tag.
    track.srclang = srclang || 'und';
    video.appendChild(track);
    return true;
}

// findSubtitleItem locates a list item by data-id without CSS.escape:
// track ids come from the torrent (file paths, stream indexes) and are
// not guaranteed to be valid selector literals.
export function findSubtitleItem(modal, id) {
    for (const el of modal.querySelectorAll('.subtitle')) {
        if (el.getAttribute('data-id') === id) return el;
    }
    return null;
}

// trackSubtitleSelect reports an activation the viewer asked for, whether
// they pressed a chip or flipped the switch: the same two events either
// way, so the ladder-level metric keeps counting every deliberate
// selection exactly once. "None" is not a track and reports nothing.
export function trackSubtitleSelect(el) {
    if (!el || !window.umami) return;
    const id = el.getAttribute('data-id');
    if (!id || id === 'none') return;
    window.umami.track('subtitle-select', selectEventData(el));
    if (el.getAttribute('data-provider') === 'UserSubtitle') window.umami.track('user-subtitle-select');
}

// setSubtitlesOff performs the switch: decide what to activate, then
// activate it. Nothing else — the activation is what writes the state
// (markTrack: the attribute, the memory of what comes back, and the muted
// redraw), so this function never touches data-subtitles-off itself.
//
// The one exception is the dead end: when the rule finds nothing
// activatable there is no activation to carry the state, so the switch is
// put back where it was from here.
//
// Returns the element it activated, or null, so the caller can report the
// choice (telemetry, the manual-choice hook).
export function setSubtitlesOff(container, modal, off) {
    if (!modal) return null;
    const audioEl = modal.querySelector('.audio[data-default="true"]');
    const decision = toggleDecision({
        on: !off,
        lastId: modal.getAttribute('data-last-subtitle') || '',
        suggestedId: suggestedSubtitleID(modal),
        tracks: readTracks(modal),
        audioLang: audioEl ? (audioEl.getAttribute('data-srclang') || '') : '',
        preferredLang: modal.getAttribute('data-preferred-lang') || '',
    });
    const item = decision.activateId ? findSubtitleItem(modal, decision.activateId) : null;
    if (!item) {
        // Nothing to turn on (no track in the viewer's language, or the only
        // one is locked). The switch must not claim otherwise: put it back
        // where it was instead of leaving it on over silence.
        applyOffState(modal, true);
        return null;
    }
    // No applyOffState here: activating the item is what moves the switch
    // (markTrack), so the state cannot be written twice and cannot disagree.
    activateSubtitle(container, item, { persist: decision.persist });
    return item;
}

// suggestedSubtitleID is the server's answer to "what would be playing if
// subtitles were on" (ListItem.Suggested, rendered as data-suggested) —
// the only thing a page opened with subtitles off has to restore from.
export function suggestedSubtitleID(modal) {
    const el = modal && modal.querySelector('.subtitle[data-suggested="true"]');
    return el ? (el.getAttribute('data-id') || '') : '';
}

// offerStorage: merely touching window.localStorage throws in a sandboxed
// frame or with site data blocked; subtitle-offer.js treats null as "no
// memory".
export function offerStorage() {
    try { return window.localStorage; } catch (e) { return null; }
}

// readUpsellCard takes the upsell's words from the picker's own card
// (#translate-cta): one sentence, one CTA, one /donate link with the
// language prefix, all rendered by the server -- so the pill's card and the
// picker's can never disagree, and no locale carries the sentence twice.
export function readUpsellCard(modal) {
    const box = modal ? modal.querySelector('#translate-cta') : null;
    const a = box ? box.querySelector('a[href]') : null;
    const text = box ? box.querySelector('div') : null;
    return {
        text: text ? text.textContent.trim() : '',
        cta: a ? a.textContent.trim() : '',
        href: a ? a.getAttribute('href') : '',
        tier: a ? a.getAttribute('data-umami-event-tier') || '' : '',
    };
}

export function findSubtitlesModal(container) {
    return (container && container.querySelector('#subtitles')) || document.getElementById('subtitles');
}

// itemData reads the fields the pure rules in subtitle-rules.js need off
// a picker list item.
export function itemData(el) {
    return {
        id: el.getAttribute('data-id') || '',
        provider: el.getAttribute('data-provider') || '',
        locked: el.getAttribute('data-locked') === 'true',
    };
}

// firstTrackSrc is the URL a side-loaded <track> is created with. For an AI
// translation it carries the viewer's position (movie time), like every poll
// does: this GET is the request that STARTS the job, and the service orders
// its first batch by the position it has on record. Without one here it read
// whatever an earlier viewing of the same file had left there -- measured
// 2026-09-18: a film opened at 0:00 got its first ~50 translated cues from
// the sixth minute, and the lines being spoken came a whole upstream call
// later. The HEAD poll does send `pos`, but it never starts a job and races
// this request. Later revisions (withRev) need none: by then the polls have
// kept the position current.
export function firstTrackSrc(video, provider, src) {
    if (provider !== 'Translated' || !src) return src;
    const offset = Number(video.dataset.runOffset) || 0;
    const at = (video.currentTime || 0) + offset;
    if (!Number.isFinite(at) || at < 0) return src;
    return withParam(src, 'pos', Math.floor(at));
}

// activateSubtitle switches playback to the subtitle the given list item
// stands for. Shared by the click handler and by the auto-selection that runs
// after an upload, so both paths create the <track>, mark the list item
// through markTrack and end with the same textTracks state.
//
// persist says whether the choice is written back to the session
// (ud.SubtitleID, which renders as ListItem.Saved). Only what the viewer did
// on purpose counts: clicking an item, or uploading a file to watch with.
// The activations the player performs for the viewer — the audio-switch
// re-pick and the mount-time restore of a saved translation — pass
// persist:false, so `Saved` keeps meaning
// exactly "the viewer chose this" and a rule the player applied for them
// never comes back as a choice the next rule has to respect.
export function activateSubtitle(container, target, { persist = true } = {}) {
    const provider = target.getAttribute('data-provider');
    const id = target.getAttribute('data-id');
    // Side-loaded tracks (OpenSubtitles, sidecar files, embed externals,
    // user uploads) exist as <track> elements only once selected — see
    // ensureTrackElement.
    if (provider !== 'MediaProbe' && id && id !== 'none') {
        const video = container.querySelector('video.player, audio.player');
        if (video) {
            // data-label only. A chip's text is decorated now — the origin
            // code ("OS"), the property tag ("forced"), the source suffix
            // ("· opensubtitles") — and that whole string used to land in
            // <track label>, which is what the native iOS track menu
            // shows. Every chip that reaches here is side-loaded and
            // carries data-label; the "None" carrier, the one without it,
            // is excluded by the id guard above. ensureTrackElement supplies
            // its own last-resort name.
            ensureTrackElement(
                video,
                id,
                firstTrackSrc(video, provider, target.getAttribute('data-src') || ''),
                target.getAttribute('data-label') || '',
                target.getAttribute('data-srclang') || '',
                target.getAttribute('data-kind') || 'subtitles',
            );
        }
    }
    markTrack(container, target, 'subtitle', persist);
    // Both halves of "what is on screen" — the hls.js selection and the
    // element modes — are written by applySubtitleSelection and nowhere
    // else, so a chip and the player cannot disagree about which of the two
    // renderers is drawing. See subtitle-apply.js for why hls.js-managed
    // tracks go to 'disabled' rather than 'hidden'.
    //
    // window.hlsPlayer is undefined until useHls creates the instance, and
    // a mount-time activation (a translation saved in an earlier session
    // coming back) routinely runs before that: then only the element modes
    // land here, and initDefaultTracks applies the same selection to hls.js
    // on the first canplay.
    const player = container.querySelector('video.player, audio.player');
    applySubtitleSelection(player, window.hlsPlayer || null, selectionFor(target));
}

export function toggleDialog(id) {
    const dialog = document.getElementById(id);
    if (!dialog || typeof dialog.showModal !== 'function') return;
    if (dialog.open) dialog.close();
    else dialog.showModal();
}

// setUploadPanel opens or closes the "My subtitles" panel. The open state
// is written on #my-subtitles, the wrapper the async swap does NOT replace
// (the toggle and the panel inside it are), so a delete or an upload can
// put the panel back the way the viewer had it.
export function setUploadPanel(modal, open) {
    const panel = modal.querySelector('#my-uploads-panel');
    if (!panel) return;
    panel.hidden = !open;
    const toggle = modal.querySelector('#my-uploads-toggle');
    if (toggle) toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
    const wrap = modal.querySelector('#my-subtitles');
    if (wrap) wrap.setAttribute('data-upload-open', open ? 'true' : 'false');
}

// isUpload is what "my uploads" means now that the MY chips live in the
// radiogroup with everything else (adoptUploadChips moves in the ones an
// async reload delivers). Containment in #my-subtitles used to be the test
// and no longer can be: that wrapper holds the disclosure and the panel,
// and — between the swap and the adoption — a fresh chip for one instant.
// The provider is the honest question anyway: these are the chips the
// uploads partial re-renders, and therefore the ones whose markers the
// client owns.
export function isUpload(el) {
    return el.getAttribute('data-provider') === 'UserSubtitle';
}

export function setUploadMark(el, on) {
    setChipActive(el, on);
    if (on) el.setAttribute('data-default', 'true');
    else el.removeAttribute('data-default');
}

// syncUploadMarks re-derives the uploads' active marker from what is
// actually playing. The <track default> in <video> drives playback
// correctly on reload, but the uploads' chips are re-rendered by the
// partial and never go through the click path, so they lose the marker;
// this runs at wiring time and after every async swap.
//
// Module scope rather than a closure inside wireTrackHandlers: it is the
// least-covered piece of this file (the stage checklist carried it as a
// by-hand item), and a function the tests can call is one they can drive
// through states a real player takes minutes to reach.
export function syncUploadMarks(container, subtitlesModal) {
    const video = container.querySelector('video.player');
    if (!video) return;
    const scope = subtitlesModal || container;
    const mine = Array.from(scope.querySelectorAll('.subtitle')).filter(isUpload);
    if (!mine.length) return;
    let activeID = null;
    for (const t of video.textTracks) {
        if (t.mode === 'showing' && t.id) { activeID = t.id; break; }
    }
    if (!activeID) {
        // The <track default> attribute answers "what is playing" only
        // while nothing else claims it. An embedded (MediaProbe) track is
        // driven by hls.js and has no <track> element at all, so "no
        // showing textTrack" does not mean "nothing is playing" — and a
        // stale default left on a side-loaded track would then hand the
        // marker to an upload as well, leaving two chips with data-default
        // and two check marks.
        for (const el of scope.querySelectorAll('.subtitle[data-default="true"]')) {
            if (!isUpload(el)) return;
        }
        const dt = video.querySelector('track[default]');
        if (dt && dt.id) activeID = dt.id;
    }
    if (!activeID) return;
    const active = mine.find((el) => el.getAttribute('data-id') === activeID) || null;
    for (const el of mine) setUploadMark(el, el === active);
    if (!active) return;
    // Exactly one marker per group, as after a click: markTrack's clearing
    // loop without the PUT — nothing was chosen here, the DOM is only
    // catching up with what is already playing.
    for (const el of scope.querySelectorAll('.subtitle')) {
        if (el === active || isUpload(el)) continue;
        setUploadMark(el, false);
    }
}

// The 'async' listener the picker installs on `window`, kept at module
// scope so wireTrackHandlers can replace it and destroyPlayer can take it
// off. See the comment at the addEventListener call below.
let asyncSwapListener = null;

// wireTrackHandlers binds the picker modals. It stays module-scope and
// ref-free: `hooks` is the object the mounted component fills with
// onSubtitleSelect/onAudioSelect (see the trackHooks effect), so the
// session-scoped state those need lives in the component, not here.
// swapSubtitlesDialog puts a freshly rendered subtitles dialog in place of
// the one on screen without touching the player (owner, 2026-09-19: a
// preferred-language change used to re-render the whole player, and a second
// of nothing is a long time in the middle of a film). The dialog ELEMENT
// stays -- it is open, and every delegated listener hangs on it; its
// attributes and its box are what the language decides, so those are what
// is replaced. Then the server's new defaults are applied the way a mount
// applies them: the subtitle the ladder chose, the audio track the language
// chose. Reports whether it swapped.
export function swapSubtitlesDialog(container, fresh, hooks = {}) {
    const live = container.querySelector('#subtitles');
    const liveBox = live && live.querySelector('.modal-box');
    const freshBox = fresh && fresh.querySelector && fresh.querySelector('.modal-box');
    if (!live || !liveBox || !freshBox) return false;
    for (const name of fresh.getAttributeNames()) {
        if (name.startsWith('data-')) live.setAttribute(name, fresh.getAttribute(name));
    }
    liveBox.replaceWith(document.importNode(freshBox, true));
    // The upload forms inside are async forms; the library binds what it is
    // told about.
    rebindAsync(live);

    const video = container.querySelector('video.player, audio.player');
    // <track>s of chips that are gone (the AI item of the old language).
    const chipIDs = [];
    for (const el of live.querySelectorAll('.subtitle')) {
        const cid = el.getAttribute('data-id');
        if (cid) chipIDs.push(cid);
    }
    dropDeletedTracks(video, chipIDs);

    const subtitle = live.querySelector('.subtitle[data-default="true"]');
    if (subtitle) {
        activateSubtitle(container, subtitle, { persist: false });
        if (hooks.onDialogSwapped) hooks.onDialogSwapped(subtitle);
    }
    const audio = live.querySelector('.audio[data-default="true"]');
    if (audio && window.hlsPlayer && audio.getAttribute('data-provider') === 'MediaProbe') {
        const idx = parseInt(audio.getAttribute('data-mp-id'));
        if (Number.isFinite(idx) && window.hlsPlayer.audioTrack !== idx) window.hlsPlayer.audioTrack = idx;
    }
    syncUploadMarks(container, live);
    applyFlagSupport(live);
    refresh(live);
    return true;
}

export function wireTrackHandlers(container, hooks = {}) {
    wirePreferredLang(container, {
        beforeRestart: () => { if (hooks.beforeRestart) hooks.beforeRestart(); },
        swap: (fresh) => swapSubtitlesDialog(container, fresh, hooks),
    });
    // Delegate subtitle clicks on #subtitles so items swapped into
    // #my-subtitles via async still work without re-binding.
    const subtitlesModal = container.querySelector('#subtitles');
    if (subtitlesModal) {
        subtitlesModal.addEventListener('click', (e) => {
            const target = e.target.closest('.subtitle');
            if (!target || !subtitlesModal.contains(target)) return;
            // A locked item (the AI translation on a free account) has no
            // Src to activate — turning it on would leave subtitles
            // "selected" with nothing on screen. Reveal the upgrade card
            // and leave the current selection untouched.
            if (target.getAttribute('data-locked') === 'true') {
                const cta = subtitlesModal.querySelector('#translate-cta');
                if (cta) cta.hidden = false;
                if (window.umami) window.umami.track('subtitle-translate-lock-click', {
                    lang: target.getAttribute('data-srclang') || '',
                });
                return;
            }
            trackSubtitleSelect(target);
            // Picking a track while the switch is off means "on, with this
            // one": one act for the viewer, so one activation and one PUT.
            // Nothing to flip here — activating a track that is not "None"
            // is what turns the switch on (markTrack).
            activateSubtitle(container, target);
            if (hooks.onSubtitleSelect) hooks.onSubtitleSelect(target);
        });

        // The subtitles switch. It is a real checkbox (DaisyUI toggle), so
        // the browser owns the pressed state and this only reacts to it —
        // change, not click, so keyboard and label clicks arrive too.
        //
        // Delegated, like the chips: the dialog's box is replaced when the
        // preferred language changes (swapSubtitlesDialog), and a listener
        // on the checkbox itself would stay behind on the old one.
        {
            subtitlesModal.addEventListener('change', (e) => {
                const toggleEl = e.target;
                if (!toggleEl || toggleEl.id !== 'subtitles-toggle') return;
                const item = setSubtitlesOff(container, subtitlesModal, !toggleEl.checked);
                // Flipping the switch is as manual as pressing a chip, and
                // the component has to hear about it: onSubtitleSelect is
                // what sets manualSubtitleRef, without which the next audio
                // switch re-runs the ladder and turns subtitles back on over
                // an explicit off. It also stops a translation poll the
                // viewer just switched away from, and starts one when the
                // track that came back is an AI item.
                if (item) {
                    trackSubtitleSelect(item);
                    if (hooks.onSubtitleSelect) hooks.onSubtitleSelect(item);
                }
            });
        }

        // Language row, "+N" and the uploads disclosure. One more delegate
        // on the same modal, for the same reason as the one above: the
        // uploads toggle is part of the markup the async swap replaces.
        subtitlesModal.addEventListener('click', (e) => {
            // A language chip never changes what is playing — it only
            // filters the track row. The one chip that does change playback
            // is "Off", and that is a .subtitle handled above.
            const lang = e.target.closest('.lang[data-lang]');
            if (lang && subtitlesModal.contains(lang)) {
                applyLangFilter(subtitlesModal, lang.getAttribute('data-lang'));
                return;
            }
            // "+N" is a toggle, not a one-way reveal: expanded it reads "×"
            // and is the only way back to the short row.
            const more = e.target.closest('#subtitle-lang-more');
            if (more && subtitlesModal.contains(more)) {
                toggleLangOverflow(subtitlesModal);
                return;
            }
            const upload = e.target.closest('#my-uploads-toggle');
            if (upload && subtitlesModal.contains(upload)) {
                const panel = subtitlesModal.querySelector('#my-uploads-panel');
                if (!panel) return;
                setUploadPanel(subtitlesModal, panel.hidden);
                return;
            }
            // The panel's own "×". Same state change as pressing the chip
            // again, through the same function: two controls, one way the
            // panel can be open or closed.
            const uploadClose = e.target.closest('#my-uploads-close');
            if (uploadClose && subtitlesModal.contains(uploadClose)) setUploadPanel(subtitlesModal, false);
        });
    }

    // Audio click handlers (no async swap — direct binding is enough).
    // e.target.closest, not e.target: a chip's click lands on the flag
    // <span> or the check <svg> as often as on the button itself, and
    // markTrack would then mark a <span> and read data-mp-id as null.
    // Delegated for the same reason as the switch above: the chips are part
    // of what a preferred-language change replaces.
    container.addEventListener('click', (e) => {
        const target = e.target.closest('.audio');
        if (!target || !container.contains(target)) return;
        markTrack(container, target, 'audio');
        if (window.hlsPlayer && target.getAttribute('data-provider') === 'MediaProbe') {
            window.hlsPlayer.audioTrack = parseInt(target.getAttribute('data-mp-id'));
        }
        if (hooks.onAudioSelect) hooks.onAudioSelect(target);
    });

    syncUploadMarks(container, subtitlesModal);
    // loadAsyncView dispatches an 'async' CustomEvent after swapping a
    // target's innerHTML; re-sync when #my-subtitles content is replaced.
    // Looked up on every event, not captured: swapSubtitlesDialog replaces
    // the dialog's box, #my-subtitles with it.
    const mySubs = () => container.querySelector('#my-subtitles');
    // Taken off again before a new one goes on, and by destroyPlayer.
    // wireTrackHandlers runs once per async navigation and the listener
    // closes over that page's #my-subtitles, so one left behind accumulates
    // and pins a detached node. Identity-guarded, so the old ones were
    // inert rather than wrong -- which is exactly why nobody noticed.
    if (asyncSwapListener) {
        window.removeEventListener('async', asyncSwapListener);
        asyncSwapListener = null;
    }
    if (mySubs()) {
        asyncSwapListener = (e) => {
            const mySubsContainer = mySubs();
            if (!mySubsContainer || !e.detail || e.detail.target !== mySubsContainer) return;
            // The panel and its toggle are part of the swapped markup; the
            // wrapper is not, so it is what remembers whether the viewer had
            // the upload form open. After a delete the viewer is still
            // looking at the panel: it must come back open, or removing two
            // files in a row means re-opening it between them.
            if (mySubsContainer.getAttribute('data-upload-open') === 'true') {
                setUploadPanel(subtitlesModal || container, true);
            }
            // The response renders the MY chips into this wrapper, which
            // sits outside the radiogroup — move them in before anything
            // reads the row. Everything below (the autoselect marker, the
            // chip ids a delete is measured against, refresh's counts and
            // filter) looks at #subtitle-tracks, and a chip still sitting
            // in the wrapper is invisible to all of it.
            adoptUploadChips(subtitlesModal || container);
            // A freshly uploaded subtitle comes back marked by the server.
            // Switch to it right away: the viewer uploaded a file to watch
            // with, and making them hunt for it in the row afterwards reads
            // as "subtitles don't work".
            const fresh = (subtitlesModal || container).querySelector('.subtitle[data-autoselect="true"]');
            if (fresh) activateSubtitle(container, fresh);
            else {
                // A delete takes the chip away but not the <track>: that
                // one lives in <video>, which this swap never touches, so
                // the deleted file's subtitles stayed on screen with
                // nothing marked and "Now:" blank. Drop the orphans and,
                // when the viewer deleted what was playing, land on "Off" —
                // the honest reading of "I removed that file". persist:
                // false, because they chose a deletion, not a track.
                const video = container.querySelector('video.player, audio.player');
                const chipIDs = [];
                for (const el of (subtitlesModal || container).querySelectorAll('.subtitle')) {
                    const cid = el.getAttribute('data-id');
                    if (cid) chipIDs.push(cid);
                }
                const wasShowing = dropDeletedTracks(video, chipIDs);
                const off = wasShowing && subtitlesModal ? findSubtitleItem(subtitlesModal, 'none') : null;
                if (off) activateSubtitle(container, off, { persist: false });
                else syncUploadMarks(container, subtitlesModal);
            }
            // The set of chips itself changed, so this is the full pass and
            // not refreshMarks: an upload can bring a language the server
            // never rendered a chip for, and a delete can empty the expanded
            // one. `current` keeps the viewer where they were whenever that
            // language survived the swap.
            if (subtitlesModal) refresh(subtitlesModal, { current: expandedLang(subtitlesModal) });
        };
        window.addEventListener('async', asyncSwapListener);
    }

    // First pass: hide the tracks of every collapsed language, drop the
    // flags where the platform draws regional indicators as letter pairs,
    // and fill both "Now:" lines. (refresh ends with applyFlagSupport of
    // its own; the explicit call is what makes the mount-time guarantee
    // independent of refresh's internals.)
    if (subtitlesModal) {
        applyFlagSupport(subtitlesModal);
        refresh(subtitlesModal);
    }
}

// markTrack moves the active marker of one group (audio or subtitle) onto
// `el`. The look is a cyan fill plus the check icon that is already in
// every chip's markup — toggled, never rebuilt: a chip carries its origin
// badge, its property tag and, on the AI item, the .tr-progress span of a
// running translation, and innerHTML would throw all three away mid-poll.
//
// `persist` is what separates a choice from a rule the player applied for
// the viewer: the audio-switch re-pick and the mount-time restore of a
// saved translation pass false and never PUT, so `Saved` keeps meaning "the viewer
// chose this". When it does persist, the write in flight is returned (see
// persistTrackChoice); no call site in the player awaits it.
export function markTrack(container, el, type, persist = true) {
    if (el.getAttribute('data-default') === 'true') return;
    const s = container.querySelector('#subtitles');
    // Read before anything moves: the switch's memory is the track being
    // replaced, and a line further down there is no way to tell which chip
    // that was.
    const prev = s && type === 'subtitle' ? s.querySelector('.subtitle[data-default="true"]') : null;
    const prevID = prev ? (prev.getAttribute('data-id') || '') : '';

    setChipActive(el, true);
    el.setAttribute('data-default', 'true');

    if (!s) return;
    const es = s.querySelectorAll(`.${type}`);
    for (const ee of es) {
        if (ee === el) continue;
        setChipActive(ee, false);
        ee.removeAttribute('data-default');
    }
    if (type === 'subtitle') {
        // The one writer of "are subtitles off": every activation moves the
        // switch with it, including the ones the player performs for the
        // viewer (a deleted upload landing on None, the audio-switch
        // re-pick, an upload selected right after it was added). Kept here
        // rather than at each call site because a second writer is exactly
        // how the switch and the row drifted apart. After the clearing loop,
        // because applyOffState puts the muted mark back.
        const next = offStateAfterActivate(prevID, el.getAttribute('data-id') || '', s.getAttribute('data-last-subtitle') || '');
        if (next.lastId) s.setAttribute('data-last-subtitle', next.lastId);
        else s.removeAttribute('data-last-subtitle');
        applyOffState(s, next.off);
    }
    // The dot on the language chip and the "Now:" line, not the language
    // filter: the viewer's expanded language is their own choice and must
    // not jump under them because playback moved. After applyOffState: with
    // subtitles off the row follows the muted choice, which the line above
    // has just named.
    refreshMarks(s);

    if (!persist) return;
    return persistTrackChoice(type, {
        id: el.getAttribute('data-id'),
        resourceID: s.getAttribute('data-resource-id'),
        itemID: s.getAttribute('data-item-id'),
    });
}

// PUT_RETRY_DELAY_MS is how long the one retry below waits. Long enough for
// an edge that is refusing connections to finish failing over, short enough
// that a viewer who closes the tab straight after clicking still has a
// decent chance of the write landing.
export const PUT_RETRY_DELAY_MS = 1000;

// persistTrackChoice writes the viewer's pick back to the session, and
// retries it once.
//
// Why: on stage two of these came back 503 from the edge without ever
// reaching the pod, and the choice was silently lost — the request is
// fire-and-forget, so nothing noticed and nothing told the viewer. A single
// retry covers the failure this actually is (an edge blip, a dropped
// connection) without turning a busy backend into a stampede.
//
// What is retried and what is not. A rejected fetch is a network error: the
// request may not have been made at all. A 5xx is the server saying it
// could not handle it. A 4xx is an answer — a stale CSRF token, a session
// that ended, an id the server refuses — and repeating it would get the
// same answer, so it is left alone. Nothing is retried twice: a second
// failure is not a blip.
//
// Still fire-and-forget: no UI, no error surface, and every failure ends
// swallowed. The returned promise is the write in flight, which is what
// makes the retry testable without a sleep in the test.
//
// A retry only ever resends the LATEST choice of its kind. The body is read
// eagerly at markTrack time, so a second later it may name a track the
// viewer has already moved on from: click A (503), click B (200), and A's
// retry would land last and make the next page load restore A. That is
// worse than the failure it exists to fix — before the retry a lost PUT
// only lost that choice; it could not overwrite a newer one. Each call
// takes the next number for its kind, and the retry stands down when it is
// no longer holding it. Audio and subtitle count separately: they are two
// independent choices and one must not cancel the other's retry.
const putSeq = new Map();

// And a retry does not outlive the player that queued it. destroyPlayer
// bumps this, so a write scheduled by a page the viewer has navigated away
// from cannot land — possibly into the next file's session.
let playerGeneration = 0;

export function persistTrackChoice(type, body) {
    const seq = (putSeq.get(type) || 0) + 1;
    putSeq.set(type, seq);
    const generation = playerGeneration;
    const stillCurrent = () => putSeq.get(type) === seq && playerGeneration === generation;
    const send = () => fetch(`/stream-video/${type}`, {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json',
            'X-CSRF-TOKEN': window._CSRF,
        },
        body: JSON.stringify(body),
    });
    const retry = () => new Promise((resolve) => setTimeout(resolve, PUT_RETRY_DELAY_MS))
        .then(() => (stillCurrent() ? send() : undefined));
    return send().then(
        (res) => (res && res.status >= 500 ? retry() : res),
        retry,
    ).catch(() => {});
}

// releaseTrackDialog is destroyPlayer's half of this module: "this page's
// player is gone" stands a queued PUT retry down (persistTrackChoice) and
// takes the picker's 'async' listener off, which wireTrackHandlers put on
// and which therefore outlives the mount.
export function releaseTrackDialog() {
    playerGeneration++;
    if (asyncSwapListener) {
        window.removeEventListener('async', asyncSwapListener);
        asyncSwapListener = null;
    }
}
