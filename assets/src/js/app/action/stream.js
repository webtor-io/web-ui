import av from '../../lib/av';

av(async function() {
    const { initPlayer } = await import('../../lib/player/Player');
    initPlayer(this);
}, async function() {
    const { destroyPlayer } = await import('../../lib/player/Player');
    destroyPlayer();
});

export {}