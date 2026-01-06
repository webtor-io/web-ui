import av from '../../lib/av';
av( async function() {
    if (!window.umami) return;
    await window.umami.track('keep-available-shown');
});
