import av from '../lib/av';

const SKIP_BYTES = 1 * 1024 * 1024; // 1MB — skip TCP slow start
const UPDATE_INTERVAL = 300; // ms between UI updates

av(function() {
    const root = this;
    const startBtn = root.querySelector('#speedtest-start');
    if (!startBtn) return;

    startBtn.addEventListener('click', () => runTest(root));

    // Auto-start when navigated with ?again
    const controls = root.querySelector('#speedtest-controls');
    if (controls && controls.hasAttribute('data-autostart')) {
        setTimeout(() => runTest(root), 0);
    }
});

async function runTest(root) {
    const controls = root.querySelector('#speedtest-controls');
    const progress = root.querySelector('#speedtest-progress');
    const speedEl = root.querySelector('#speedtest-speed');
    const bar = root.querySelector('#speedtest-bar');
    const statusEl = root.querySelector('#speedtest-status');
    const phaseEl = root.querySelector('#speedtest-phase');
    const form = root.querySelector('#speedtest-form');
    const speedInput = root.querySelector('#speedtest-speed-input');
    const premiumSpeedInput = root.querySelector('#speedtest-premium-speed-input');

    controls.classList.add('hidden');
    progress.classList.remove('hidden');
    speedEl.textContent = '0.0';
    bar.value = 0;
    statusEl.textContent = 'Getting test server...';
    if (phaseEl) phaseEl.textContent = '';

    try {
        const urlRes = await fetch('/speedtest/url');
        if (!urlRes.ok) throw new Error('Failed to get speedtest URL');
        const { urls } = await urlRes.json();

        let standardSpeed = 0;
        let premiumSpeed = 0;

        for (const entry of urls) {
            const isPremium = entry.type === 'premium';

            if (phaseEl) {
                phaseEl.textContent = isPremium ? 'Premium Server' : 'Standard Server';
                phaseEl.className = isPremium
                    ? 'text-sm font-semibold text-w-purpleL mb-1'
                    : 'text-sm font-semibold text-w-cyan mb-1';
            }
            statusEl.textContent = isPremium
                ? 'Testing premium server...'
                : 'Measuring speed...';
            speedEl.textContent = '0.0';
            bar.value = 0;

            const speed = await measure(entry.url, speedEl, bar);

            if (isPremium) {
                premiumSpeed = speed;
            } else {
                standardSpeed = speed;
            }
        }

        speedInput.value = standardSpeed.toFixed(1);
        if (premiumSpeedInput) {
            premiumSpeedInput.value = premiumSpeed.toFixed(1);
        }
        form.requestSubmit();
    } catch (err) {
        statusEl.textContent = 'Error: ' + err.message;
        setTimeout(() => {
            progress.classList.add('hidden');
            controls.classList.remove('hidden');
        }, 3000);
    }
}

async function measure(url, speedEl, bar) {
    const response = await fetch(url);
    if (!response.ok) throw new Error('Speedtest server error');

    const contentLength = parseInt(response.headers.get('content-length') || '0', 10);
    const reader = response.body.getReader();

    let totalBytes = 0;
    let measureStart = null;
    let measuredBytes = 0;
    let lastUpdate = 0;
    let currentSpeed = 0;

    while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        totalBytes += value.length;

        if (contentLength > 0) {
            bar.value = Math.round((totalBytes / contentLength) * 100);
        }

        if (!measureStart && totalBytes >= SKIP_BYTES) {
            measureStart = performance.now();
            measuredBytes = 0;
        } else if (measureStart) {
            measuredBytes += value.length;
            const now = performance.now();
            if (now - lastUpdate >= UPDATE_INTERVAL) {
                const elapsed = (now - measureStart) / 1000;
                if (elapsed > 0) {
                    currentSpeed = (measuredBytes * 8) / (elapsed * 1_000_000);
                    speedEl.textContent = currentSpeed.toFixed(1);
                }
                lastUpdate = now;
            }
        }
    }

    if (measureStart && measuredBytes > 0) {
        const elapsed = (performance.now() - measureStart) / 1000;
        currentSpeed = (measuredBytes * 8) / (elapsed * 1_000_000);
    }

    bar.value = 100;
    return currentSpeed;
}

export {};
