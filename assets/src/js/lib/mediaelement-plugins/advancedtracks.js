Object.assign(MediaElementPlayer.prototype, {
    toggleOpenSubtitles(e) {
        const target = e.target;
        const el = this.tracksLayer.querySelector('#opensubtitles');
        const ele = this.tracksLayer.querySelector('#embedded');
        const hidden = el.classList.contains('hidden');
        if (hidden) {
            target.classList.remove('btn-outline');
            el.classList.remove('hidden');
            ele.classList.add('hidden');
        } else {
            target.classList.add('btn-outline');
            el.classList.add('hidden');
            ele.classList.remove('hidden');
        }
    },
    async markTrack(e, type) {
        if (e.getAttribute('data-default') === 'true') {
            return;
        }
        e.classList.add('text-primary', 'underline');
        e.setAttribute('data-default', 'true');
        const s = this.tracksLayer.querySelector('#subtitles');
        const es = s.querySelectorAll(`.${type}`);
        for (const ee of es) {
            if (ee === e) continue;
            ee.classList.remove('text-primary', 'underline');
            ee.removeAttribute('data-default');
        }
        await fetch(`/stream-video/${type}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-TOKEN': window._CSRF,
            },
            body:   JSON.stringify({
                id:         e.getAttribute('data-id'),
                resourceID: s.getAttribute('data-resource-id'),
                itemID:     s.getAttribute('data-item-id'),
            }),
        });
    },
    setSubtitle(e){
        const target = e.target;
        this.markTrack(target, 'subtitle');
        const videos = document.querySelectorAll('video.player');

        const hlsPlayer = window.hlsPlayer;
        const provider = target.getAttribute('data-provider');
        const id = target.getAttribute('data-id');
        const mpId = target.getAttribute('data-mp-id');

        if (hlsPlayer && provider === 'MediaProbe') {
            // Embedded HLS subtitle — let HLS.js manage text track modes
            hlsPlayer.subtitleDisplay = true;
            hlsPlayer.subtitleTrack = parseInt(mpId);
            // Hide non-MediaProbe HTML5 text tracks
            for (const p of videos) {
                for (const t of p.textTracks) {
                    if (t.id) t.mode = 'hidden';
                }
            }
        } else {
            // "None" or non-MediaProbe (ExportTag/OpenSubtitles/External)
            if (hlsPlayer) {
                hlsPlayer.subtitleTrack = -1;
                hlsPlayer.subtitleDisplay = false;
            }
            for (const p of videos) {
                for (const t of p.textTracks) {
                    t.mode = (id && id !== 'none' && t.id === id) ? 'showing' : 'hidden';
                }
            }
        }
    },
    setAudio(e) {
        const target = e.target;
        this.markTrack(target, 'audio');
        const provider = target.getAttribute('data-provider');
        if (window.hlsPlayer && provider === 'MediaProbe') {
            window.hlsPlayer.audioTrack = parseInt(target.getAttribute('data-mp-id'));
        }
    },
    async buildadvancedtracks(player, controls, layers) {
        player.tracksButton = document.createElement('div');
        player.tracksButton.className = `${this.options.classPrefix}button ${this.options.classPrefix}captions-button`;
        player.tracksButton.innerHTML =
            `<button type="button" role="button" aria-owns="${this.id}" tabindex="0">
                <svg xmlns="http://www.w3.org/2000/svg" id="mep_0-icon-captions" class="mejs__icon-captions" aria-hidden="true" focusable="false">
                    <use xlink:href="assets/mejs-controls.svg#icon-captions"></use>
                </svg>
            </button>`;
        this.addControlElement(player.tracksButton, 'tracks');
        player.tracksLayer = document.createElement('div');
        player.tracksLayer.className = `${this.options.classPrefix}layer ${this.options.classPrefix}overlay ${this.options.classPrefix}tracks`;
        const tracksContainer = document.getElementById('subtitles').cloneNode(true);
        const checkbox = document.getElementById('subtitles-checkbox').cloneNode(true);
        player.tracksLayer.appendChild(checkbox);
        player.tracksLayer.appendChild(tracksContainer);
        const playLayer = layers.querySelector(`.${this.options.classPrefix}overlay-play`);

        layers.insertBefore(player.tracksLayer, playLayer);
        const t = (e) => {
            e.preventDefault();
            checkbox.checked = !checkbox.checked;
            if (checkbox.checked) {
                player.tracksLayer.style.display = 'block';
            } else {
                player.tracksLayer.style.display = 'none';
            }
            return false;
        }
        const close = tracksContainer.querySelector('label[for=subtitles-checkbox]');
        player.tracksLayer.style.display = 'none';
        player.tracksLayer.style.zIndex = 1000;
        player.tracksButton.addEventListener('click', t);
        close.addEventListener('click', t);

        for (const sub of tracksContainer.querySelectorAll('.subtitle')) {
           sub.addEventListener('click',  this.setSubtitle.bind(this));
        }
        for (const audio of tracksContainer.querySelectorAll('.audio')) {
            audio.addEventListener('click',  this.setAudio.bind(this));
        }
        const opensubtitles = tracksContainer.querySelector('label[for=opensubtitles]');
        if (opensubtitles) {
            opensubtitles.addEventListener('click', this.toggleOpenSubtitles.bind(this));
        }
    },
})