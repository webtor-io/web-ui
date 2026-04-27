import av from '../../lib/av';
av( async function() {
    if (!window.umami) return;
    const el = this.querySelector('[data-vault-state]');
    const state = el && el.dataset.vaultState;
    if (state === 'authed') {
        await window.umami.track('vault-shown');
    } else if (state === 'anon') {
        await window.umami.track('vault-shown-anonymous');
    }
});
