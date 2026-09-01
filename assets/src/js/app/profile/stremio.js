import av from '../../lib/av';

// One-click install. The fresh block's form mints the addon token; the block
// then re-renders (async island) into the token state. The user's intent —
// "install", not merely "give me the link" — has to survive that round-trip,
// and the fragment swap is a new document subtree, so it rides in
// sessionStorage: set on submit, consumed by the next render of this module,
// which opens the stremio:// deep link exactly once.
//
// Without JS: the form posts, the page reloads into the token state, and the
// Install button is the first thing in it.
const PENDING = 'stremio-install-pending';

function setPending(on) {
    try {
        if (on) sessionStorage.setItem(PENDING, '1');
        else sessionStorage.removeItem(PENDING);
    } catch (e) { /* storage blocked: degrades to two clicks */ }
}

function takePending() {
    try {
        const v = sessionStorage.getItem(PENDING) === '1';
        sessionStorage.removeItem(PENDING);
        return v;
    } catch (e) {
        return false;
    }
}

av(function() {
    const form = this.querySelector('form[data-stremio-generate]');
    if (form) {
        form.addEventListener('submit', (e) => {
            // e.submitter: which of the two submit buttons was used. Older
            // engines without it fall back to "install" — the primary action.
            const btn = e.submitter;
            setPending(!btn || btn.hasAttribute('data-stremio-install'));
        });
    }
    const link = this.querySelector('[data-stremio-deeplink]');
    if (link && takePending()) {
        if (window.umami) {
            window.umami.track('stremio-install-addon', { stage: 'auto' });
        }
        window.location.href = link.getAttribute('href');
    }
});

export {}
