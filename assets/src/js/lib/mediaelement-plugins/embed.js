Object.assign(MediaElementPlayer.prototype, {
    async buildembed(player, controls, layers) {
        player.embedButton = document.createElement('div');
        player.embedButton.className = `${this.options.classPrefix}button ${this.options.classPrefix}embed-button`;
        player.embedButton.innerHTML =
            `<button type="button" role="button" aria-owns="${this.id}" tabindex="0">&lt;<span class="slash">/</span>&gt;</button>`;
        this.addControlElement(player.embedButton, 'embed');
        player.embedLayer = document.createElement('div');
        player.embedLayer.className = `${this.options.classPrefix}layer ${this.options.classPrefix}overlay ${this.options.classPrefix}webtor-embed`;
        const embedContainer = document.getElementById('embed').cloneNode(true);
        const checkbox = document.getElementById('embed-checkbox').cloneNode(true);
        player.embedLayer.appendChild(checkbox);
        player.embedLayer.appendChild(embedContainer);
        const playLayer = layers.querySelector(`.${this.options.classPrefix}overlay-play`);
        layers.insertBefore(player.embedLayer, playLayer);
        const t = (e) => {
            e.preventDefault();
            checkbox.checked = !checkbox.checked;
            if (checkbox.checked) {
                player.embedLayer.style.display = 'block';
            } else {
                player.embedLayer.style.display = 'none';
            }
            return false;
        }

        for (const cl of embedContainer.querySelectorAll('label[for=embed-checkbox]')) {
            cl.addEventListener('click', t);
        }
        const copy = embedContainer.querySelector('label.copy');
        copy.addEventListener('click', () => {
            const code = embedContainer.querySelector('textarea').value;
            navigator.clipboard.writeText(code);
            if (window.toast) window.toast.success('Copied!');
        });
        player.embedLayer.style.display = 'none';
        player.embedLayer.style.zIndex = 1000;
        player.embedButton.addEventListener('click', t);
    },
})