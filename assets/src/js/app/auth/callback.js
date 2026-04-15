import av from '../../lib/av';
av(async function() {
    const processAuth = (await import('./processAuth')).processAuth;
    await processAuth(this, 'callback', 'auth.progress.checkingAuthCode', 'handleCallback');
});

export {}
