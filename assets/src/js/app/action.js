import av from '../lib/av';
av(async function() {
    const self = this;
    const progress = self.querySelector('.progress-alert');
    const el = document.createElement('div');
    const initProgressLog = (await import('../lib/progressLog')).initProgressLog;
    initProgressLog(progress, function(ev) {
        if (ev.level !== 'rendertemplate') return;
        // The rendered player is kept out of sight, under the job's log,
        // until it can play (player_ready: canplay) -- or until it has
        // something the viewer must see before that (player_show: the stream
        // restart's card, lib/player/Player.jsx; a restarted job whose new
        // session is dead at its first request never gets to canplay, and
        // its card sat hidden under "waiting for the player" for minutes).
        const reveal = function() {
            window.removeEventListener('player_ready', reveal);
            window.removeEventListener('player_show', reveal);
            progress.classList.add('hidden');
            el.classList.remove('hidden');
        };
        window.addEventListener('player_ready', reveal);
        window.addEventListener('player_show', reveal);
        el.classList.add('hidden');
        el.classList.add('mb-5')
        self.appendChild(el);
        ev.render(el);
    });
});

export {}
