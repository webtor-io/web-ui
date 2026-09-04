import av from '../lib/av';

// The hero section is re-rendered on every async nav into "main", so the
// pointer listeners have to be torn down with it — hence the destroy hook.
let destroyHeroPointer = null;

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
}, function() {
    if (destroyHeroPointer) {
        destroyHeroPointer();
        destroyHeroPointer = null;
    }
});

export {}