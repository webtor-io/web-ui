import av from '../lib/av';

// The hero section is re-rendered on every async nav into "main", so the
// pointer listeners have to be torn down with it — hence the destroy hook.
let destroyHeroPointer = null;
let destroyContinueWatching = null;

// x on a Continue Watching card: hide the title (the server keeps the watch
// progress) and drop the card, and the whole ribbon once it is empty.
function initContinueWatching(root) {
    const onClick = async (e) => {
        const arrow = e.target.closest('[data-cw-scroll]');
        if (arrow) {
            // The row hides its scrollbar and a mouse wheel scrolls the page,
            // so the arrows are how a long row is reached.
            const track = arrow.closest('#continue-watching-ribbon')?.querySelector('[data-cw-track]');
            if (track) track.scrollBy({ left: Number(arrow.dataset.cwScroll) * track.clientWidth * 0.8, behavior: 'smooth' });
            return;
        }
        const btn = e.target.closest('[data-cw-dismiss]');
        if (!btn) return;
        e.preventDefault();
        e.stopPropagation();
        btn.disabled = true;
        try {
            const res = await fetch('/watch/dismiss', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', 'X-CSRF-TOKEN': window._CSRF },
                body: JSON.stringify({ resource_id: btn.dataset.resourceId }),
            });
            if (!res.ok) throw new Error('dismiss failed');
            const card = btn.closest('[data-cw-card]');
            const ribbon = btn.closest('#continue-watching-ribbon');
            card?.remove();
            if (ribbon && !ribbon.querySelector('[data-cw-card]')) ribbon.remove();
        } catch (err) {
            btn.disabled = false;
        }
    };
    root.addEventListener('click', onClick, true);
    return () => root.removeEventListener('click', onClick, true);
}

av(async function() {
    const dropzone = this.querySelector('.dropzone');
    if (dropzone) {
        const initDrop = (await import('../lib/drop')).initDrop;
        initDrop(dropzone);
    }
    // The load job's log host is initialised by load.js, shipped with
    // partials/load/progress.html — so a retry that re-renders the host
    // into #log-load gets the same init without this view being reloaded.
    const initHeroPointer = (await import('../lib/heroPointer')).initHeroPointer;
    destroyHeroPointer = initHeroPointer(this);
    destroyContinueWatching = initContinueWatching(this);
}, function() {
    if (destroyContinueWatching) {
        destroyContinueWatching();
        destroyContinueWatching = null;
    }
    if (destroyHeroPointer) {
        destroyHeroPointer();
        destroyHeroPointer = null;
    }
});

export {}