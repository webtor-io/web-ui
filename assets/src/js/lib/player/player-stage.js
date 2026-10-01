// The stage owns fullscreen and outlives both episode changes and stream recovery.
// Only the rendered player's wrapper is replaced; scripts are already loaded.
export async function replacePlayerOnStage(doc, { stage, root, aspectRatio, initPlayer, destroyPlayer, ...opts }) {
    // The stage is empty between the two players, and an empty block has
    // no height: the page below jumped up and back (owner). Hold the
    // height it has now until the new player is ready, and say "loading"
    // inside it meanwhile.
    if (stage) {
        stage.style.minHeight = `${stage.offsetHeight}px`;
        // --switching holds the height; --empty is the spinner, and only
        // for as long as there is no player in the stage to show its own.
        stage.classList.add('wt-player-stage--switching', 'wt-player-stage--empty');
        const release = () => {
            window.removeEventListener('player_ready', release);
            clearTimeout(timer);
            stage.style.minHeight = '';
            stage.classList.remove('wt-player-stage--switching', 'wt-player-stage--empty');
        };
        const timer = setTimeout(release, 15000);
        window.addEventListener('player_ready', release);
    }
    destroyPlayer({ keepStage: true });
    const host = stage ? stage.parentNode : root;
    for (const child of [...host.children]) {
        if (child !== stage) child.remove();
    }
    // Scripts in the render are inert when adopted this way, which is
    // wanted: the player is initialised here, by hand, on the stage.
    //
    // What is adopted is the CONTENT of the render's own wrapper (the
    // <div class="relative"> around the video and its dialogs), into the
    // old wrapper that holds the stage: adopting the wrapper itself nested
    // one more level on every move. The stylesheet and script that follow
    // the wrapper in the render are already on the page.
    const player = doc.querySelector('.player');
    const from = player && player.parentNode ? player.parentNode : doc.body;
    for (const node of [...from.childNodes]) {
        if (node.nodeType === 1 && (node.tagName === 'SCRIPT' || node.tagName === 'LINK')) continue;
        host.appendChild(document.importNode(node, true));
    }
    await initPlayer(host, { ...opts, stage, aspectRatio });
    try {
        if (stage) stage.classList.remove('wt-player-stage--empty');
        window.dispatchEvent(new CustomEvent('player_replaced', { detail: { target: host } }));
    } catch (e) {
        console.error('player: after-mount step failed', e);
    }
    return host;
}
