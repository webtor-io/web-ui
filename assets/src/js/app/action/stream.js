import av from '../../lib/av';

// Wires drag-and-drop + auto-submit on the "My Subtitles" upload form.
// Uses event delegation on the stable #subtitles modal so rebinding after
// an async swap of #my-subtitles isn't needed — listeners survive.
function initUserSubtitleUpload() {
    const modal = document.querySelector('#subtitles');
    if (!modal) return () => {};

    const findLabel = (t) => (t && t.closest ? t.closest('.user-subtitle-form label.upload-dashed') : null);
    const highlight = (label, on) => label && label.classList.toggle('upload-dashed-active', on);

    const onDragEnter = (e) => { const l = findLabel(e.target); if (!l) return; e.preventDefault(); highlight(l, true); };
    const onDragOver  = (e) => { const l = findLabel(e.target); if (!l) return; e.preventDefault(); highlight(l, true); };
    const onDragLeave = (e) => { const l = findLabel(e.target); if (!l) return; e.preventDefault(); highlight(l, false); };
    const trackUpload = (source) => {
        if (window.umami) window.umami.track('user-subtitle-upload', { source });
    };
    const onDrop = (e) => {
        const l = findLabel(e.target);
        if (!l) return;
        e.preventDefault();
        highlight(l, false);
        const form = l.closest('.user-subtitle-form');
        const input = form && form.querySelector('.user-subtitle-input');
        if (!form || !input || !e.dataTransfer || !e.dataTransfer.files || !e.dataTransfer.files.length) return;
        input.files = e.dataTransfer.files;
        trackUpload('drop');
        form.requestSubmit();
    };
    const onChange = (e) => {
        if (!e.target.classList.contains('user-subtitle-input')) return;
        const form = e.target.closest('.user-subtitle-form');
        if (form && e.target.files && e.target.files.length) {
            trackUpload('picker');
            form.requestSubmit();
        }
    };

    modal.addEventListener('dragenter', onDragEnter);
    modal.addEventListener('dragover', onDragOver);
    modal.addEventListener('dragleave', onDragLeave);
    modal.addEventListener('drop', onDrop);
    modal.addEventListener('change', onChange);

    return () => {
        modal.removeEventListener('dragenter', onDragEnter);
        modal.removeEventListener('dragover', onDragOver);
        modal.removeEventListener('dragleave', onDragLeave);
        modal.removeEventListener('drop', onDrop);
        modal.removeEventListener('change', onChange);
    };
}

let destroyUserSubtitleUpload = () => {};

av(async function() {
    const { initPlayer } = await import('../../lib/player/Player');
    await initPlayer(this);
    destroyUserSubtitleUpload = initUserSubtitleUpload();
}, async function() {
    const { destroyPlayer } = await import('../../lib/player/Player');
    destroyPlayer();
    destroyUserSubtitleUpload();
    destroyUserSubtitleUpload = () => {};
});

export {}
