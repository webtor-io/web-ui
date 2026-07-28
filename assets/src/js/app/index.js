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
    const progress = this.querySelector('.progress-alert');
    if (progress != null) {
        const initProgressLog = (await import('../lib/progressLog')).initProgressLog;
        initProgressLog(progress);
    }
    const initHeroPointer = (await import('../lib/heroPointer')).initHeroPointer;
    destroyHeroPointer = initHeroPointer(this);
}, function() {
    if (destroyHeroPointer) {
        destroyHeroPointer();
        destroyHeroPointer = null;
    }
});

export {}