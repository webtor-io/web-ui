import av from '../../lib/av';
av(async function() {
    const processAuth = (await import('./processAuth')).processAuth;
    await processAuth(this, 'verify', 'auth.progress.checkingMagicLink', 'handleMagicLinkClicked');
});

export {}
