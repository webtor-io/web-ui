import av from '../../lib/av';
av( async function() {
    const processAuth = (await import('./processAuth')).processAuth;
    await processAuth(this, 'logout', 'auth.progress.loggingOut', 'logout');
});

export {}
