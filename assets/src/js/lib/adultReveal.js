// Per-card click-to-reveal for adult-classified poster cards.
//
// The default poster URL (/lib/poster/<rid>/<file>) returns a blurred
// image for resources with resource_metadata.is_adult=true. Users
// with UserSettings.ShowAdult=true get the unblurred /raw/ variant
// server-side and bypass this module entirely (window._showAdult
// guards every entry point below).
//
// Everyone else can tap the 18+ badge on a card to reveal it:
//   - the click swaps the card's <img> src to /lib/poster/raw/...
//   - the resource_id is recorded in localStorage so the reveal
//     persists across page-loads on the same browser
//   - subsequent renders (initial SSR + data-async fragment reloads)
//     replay the stored reveals in place
//
// Anonymous users still see the click target but tapping it 401s on
// /raw/ — the trade-off is keeping the UI consistent rather than
// branching the badge render on auth state.

const STORAGE_KEY = 'w-adult-revealed';
const MAX_ENTRIES = 500;

function loadReveals() {
    try {
        const arr = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        return new Set(Array.isArray(arr) ? arr : []);
    } catch (e) {
        return new Set();
    }
}

function saveReveals(set) {
    // Bound the storage so a power-user tapping reveals across years
    // of activity doesn't grow the entry past the localStorage 5MB
    // quota. Drop oldest first.
    const arr = [...set];
    if (arr.length > MAX_ENTRIES) {
        arr.splice(0, arr.length - MAX_ENTRIES);
    }
    try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(arr));
    } catch (e) {
        // Quota exceeded or storage disabled — fail silently. The
        // user keeps the reveal for the current session only.
    }
}

function revealCard(badgeWrapper) {
    const card = badgeWrapper.parentElement;
    if (!card) return;
    const img = card.querySelector('img[src*="/lib/poster/"]');
    if (img && img.src.indexOf('/lib/poster/raw/') === -1 && img.src.indexOf('/og.jpg') === -1) {
        img.src = img.src.replace('/lib/poster/', '/lib/poster/raw/');
    }
    badgeWrapper.remove();
}

// apply replays stored reveals over a subtree (or the whole document
// when root is undefined). Called on initial load and on every async
// fragment rebind so freshly-rendered badges in async-loaded HTML
// pick up the same reveals.
export function apply(root) {
    if (window._showAdult) return;
    const target = root || document;
    const reveals = loadReveals();
    target.querySelectorAll('.w-adult-badge[data-resource-id]').forEach((badge) => {
        if (reveals.has(badge.dataset.resourceId)) {
            revealCard(badge);
        }
    });
}

// install wires the click capture handler + the async-rebind listener.
// Idempotent only by virtue of being called once at module init — call
// sites shouldn't invoke it more than once.
export function install() {
    // Delegated click handler. Caught at capture so the wrapping <a>
    // around each card doesn't navigate before we get to the badge.
    document.addEventListener('click', (e) => {
        if (window._showAdult) return;
        const span = e.target.closest('.w-adult-badge > span');
        if (!span) return;
        const wrapper = span.parentElement;
        const rid = wrapper && wrapper.dataset.resourceId;
        if (!rid) return;
        e.preventDefault();
        e.stopPropagation();
        const reveals = loadReveals();
        reveals.add(rid);
        saveReveals(reveals);
        revealCard(wrapper);
    }, { capture: true });

    // lib/async.js dispatches 'async' on window after rebinding a
    // fragment; reapply scoped to the rebound subtree so newly
    // rendered badges pick up the user's stored reveals.
    window.addEventListener('async', (e) => apply(e.detail && e.detail.target));

    // layout.js is included at end of <body> so DOMContentLoaded has
    // already fired by the time module init runs. Apply directly
    // instead of subscribing to an event that will never re-fire.
    apply();
}
