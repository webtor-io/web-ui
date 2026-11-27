import av from '../../lib/av';
import { humanBytes } from '../../lib/humanBytes';

// Partial-archive selection mode for the file browser card. Selection is
// keyed per (resource, directory) at module level so it survives the async
// #list reloads pagination triggers, and resets on directory change or full
// page load. Checked paths ride to /download-dir as repeated hidden
// "paths" inputs appended to BOTH archive forms (TAR and ZIP live in
// separate forms because async.js drops the submitter value).
const state = new Map();

// Server-side bounds mirrored from handlers/action/handler.go: entry count
// and percent-encoded byte length the selection adds to the signed
// download URL (edge proxies cap request lines at ~8k).
const MAX_PATHS = 1024;
const MAX_ENCODED_LEN = 6000;

// Drops entries covered by a selected ancestor folder (a checked folder
// already spans its subtree), then sorts — the selection is a set, and a
// canonical order keeps the job cache key and archive ETag stable. Without
// the pruning a folder + a file inside it would double-count the size sum.
function pruneNested(paths) {
    return paths
        .filter((p) => !paths.some((q) => q !== p && p.startsWith(q + '/')))
        .sort();
}

function encodedLen(paths) {
    let total = 0;
    for (const p of paths) total += encodeURIComponent(p).length + '&paths='.length;
    return total;
}

av(function () {
    const card = this;
    const toggle = card.querySelector('.js-select-toggle');
    const boxes = card.querySelectorAll('.js-select-box');
    if (!toggle || !boxes.length) return;
    toggle.classList.remove('hidden');

    const key = `${card.dataset.resourceId}:${card.dataset.dir}`;
    if (!state.has(key)) state.set(key, { active: false, sel: new Map() });
    const st = state.get(key);

    const sizeEls = card.querySelectorAll('.js-archive-size');
    const forms = card.querySelectorAll('form.js-archive-form');
    const fullSize = sizeEls.length ? sizeEls[0].textContent : '';

    function apply() {
        toggle.querySelector('.js-select-off').classList.toggle('hidden', st.active);
        toggle.querySelector('.js-select-on').classList.toggle('hidden', !st.active);
        for (const box of boxes) {
            box.classList.toggle('hidden', !st.active);
            const cb = box.querySelector('input');
            cb.checked = st.sel.has(cb.dataset.path);
        }
        const pruned = pruneNested([...st.sel.keys()]);
        // Empty selection keeps the whole-directory download semantics —
        // same as leaving select mode.
        const hasSel = st.active && pruned.length > 0;
        let total = 0;
        for (const p of pruned) total += st.sel.get(p);
        for (const el of sizeEls) el.textContent = hasSel ? humanBytes(total) : fullSize;
        for (const form of forms) {
            for (const input of form.querySelectorAll('input[name="paths"]')) input.remove();
            if (!hasSel) continue;
            for (const p of pruned) {
                const input = document.createElement('input');
                input.type = 'hidden';
                input.name = 'paths';
                input.value = p;
                form.appendChild(input);
            }
        }
    }

    toggle.addEventListener('click', function () {
        st.active = !st.active;
        if (!st.active) st.sel.clear();
        apply();
    });
    card.addEventListener('change', function (e) {
        const cb = e.target.closest('.js-select-box input');
        if (!cb) return;
        if (cb.checked) {
            st.sel.set(cb.dataset.path, Number(cb.dataset.size) || 0);
            const pruned = pruneNested([...st.sel.keys()]);
            if (pruned.length > MAX_PATHS || encodedLen(pruned) > MAX_ENCODED_LEN) {
                // Over the server-side bound — refuse this tick instead of
                // letting the submit fail later.
                st.sel.delete(cb.dataset.path);
                cb.checked = false;
                return;
            }
        } else {
            st.sel.delete(cb.dataset.path);
        }
        apply();
    });
    apply();
});
