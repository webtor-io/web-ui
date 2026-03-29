const SKIP_BYTES = 1 * 1024 * 1024; // 1MB — skip TCP slow start
const UPDATE_INTERVAL = 300; // ms between UI updates

const QUALITY_TIERS = [
    { name: '480p SD',              bitrate: 1.5,  minSpeed: 2.25  },
    { name: '720p HD',              bitrate: 3,    minSpeed: 4.5   },
    { name: '1080p Full HD',        bitrate: 6,    minSpeed: 9     },
    { name: '1080p High Bitrate',   bitrate: 10,   minSpeed: 15    },
    { name: '4K Ultra HD',          bitrate: 25,   minSpeed: 37.5  },
];

const PLANS = [
    { name: 'Free',    speed: 5,   label: '5 Mbps'       },
    { name: 'Bronze',  speed: 20,  label: '20 Mbps'      },
    { name: 'Silver',  speed: 50,  label: '50 Mbps'      },
    { name: 'Gold',    speed: 100, label: '100 Mbps'     },
];

function init() {
    const app = document.getElementById('speedtest-app');
    if (!app) return;

    const userTier = (app.dataset.tier || 'free').toLowerCase();
    const rateLimit = parseInt(app.dataset.rateLimit || '0', 10);

    const startBtn = document.getElementById('speedtest-start');
    const againBtn = document.getElementById('speedtest-again');

    startBtn.addEventListener('click', () => runTest(userTier, rateLimit));
    againBtn.addEventListener('click', () => runTest(userTier, rateLimit));
}

async function runTest(userTier, rateLimit) {
    const controls = document.getElementById('speedtest-controls');
    const progress = document.getElementById('speedtest-progress');
    const results = document.getElementById('speedtest-results');
    const speedEl = document.getElementById('speedtest-speed');
    const bar = document.getElementById('speedtest-bar');
    const statusEl = document.getElementById('speedtest-status');

    // Show progress, hide others
    controls.classList.add('hidden');
    results.classList.add('hidden');
    progress.classList.remove('hidden');
    speedEl.textContent = '0.0';
    bar.value = 0;
    statusEl.textContent = 'Getting test server...';

    try {
        // Step 1: Get speedtest URL from web-ui backend
        const urlRes = await fetch('/speedtest/url');
        if (!urlRes.ok) throw new Error('Failed to get speedtest URL');
        const { url } = await urlRes.json();

        statusEl.textContent = 'Measuring speed...';

        // Step 2: Download and measure
        const speedMbps = await measure(url, speedEl, bar, statusEl);

        // Step 3: Show results
        progress.classList.add('hidden');
        showResults(speedMbps, userTier, rateLimit);
    } catch (err) {
        statusEl.textContent = 'Error: ' + err.message;
        // Re-show start button after a delay
        setTimeout(() => {
            progress.classList.add('hidden');
            controls.classList.remove('hidden');
        }, 3000);
    }
}

async function measure(url, speedEl, bar, statusEl) {
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

        // Update progress bar
        if (contentLength > 0) {
            bar.value = Math.round((totalBytes / contentLength) * 100);
        }

        // Skip initial bytes for TCP slow start
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
                    statusEl.textContent = 'Measuring speed...';
                }
                lastUpdate = now;
            }
        }
    }

    // Final calculation
    if (measureStart && measuredBytes > 0) {
        const elapsed = (performance.now() - measureStart) / 1000;
        currentSpeed = (measuredBytes * 8) / (elapsed * 1_000_000);
    }

    bar.value = 100;
    return currentSpeed;
}

function showResults(speedMbps, userTier, rateLimit) {
    const results = document.getElementById('speedtest-results');
    const finalSpeed = document.getElementById('speedtest-final-speed');
    const qualityEl = document.getElementById('speedtest-quality');
    const plansEl = document.getElementById('speedtest-plans');
    const rateNote = document.getElementById('speedtest-rate-note');

    finalSpeed.textContent = speedMbps.toFixed(1);

    // Quality tiers
    qualityEl.innerHTML = QUALITY_TIERS.map(tier => {
        const ok = speedMbps >= tier.minSpeed;
        return `<div class="flex items-center gap-3 p-3 rounded-lg ${ok ? 'bg-base-200' : 'bg-base-200/50 opacity-50'}">
            <span class="${ok ? 'text-green-400' : 'text-w-muted'} text-lg">${ok ? '&#10003;' : '&#10007;'}</span>
            <span class="flex-1 font-medium">${tier.name}</span>
            <span class="text-w-sub text-sm">needs ${tier.minSpeed} Mbps</span>
        </div>`;
    }).join('');

    // Plan recommendations
    plansEl.innerHTML = PLANS.map(plan => {
        const isCurrent = plan.name.toLowerCase() === userTier;
        const canSupport = plan.speed === 0 || speedMbps >= plan.speed;
        const ring = isCurrent ? 'ring-2 ring-w-cyan bg-w-cyan/5' : '';
        const badge = isCurrent
            ? '<span class="badge badge-sm bg-w-cyan/20 text-w-cyan border-0">Your plan</span>'
            : (canSupport && plan.speed > 0
                ? '<a href="/donate" class="link text-w-cyan text-xs">Upgrade</a>'
                : '');
        const icon = canSupport
            ? '<span class="text-green-400">&#10003;</span>'
            : '<span class="text-w-muted">&#10007;</span>';

        return `<div class="flex items-center gap-3 p-3 rounded-lg bg-base-200 ${ring}">
            ${icon}
            <span class="flex-1 font-medium">${plan.name}</span>
            <span class="text-w-sub text-sm">${plan.label}</span>
            ${badge}
        </div>`;
    }).join('');

    // Rate limit detection
    if (rateLimit > 0) {
        const rateLimitMbps = rateLimit;
        // If measured speed is within 90% of rate limit, likely throttled
        if (speedMbps >= rateLimitMbps * 0.9) {
            rateNote.textContent = `Your speed may be limited by your current plan (${rateLimitMbps} Mbps). Consider upgrading for faster streaming.`;
            rateNote.classList.remove('hidden');
        } else {
            rateNote.classList.add('hidden');
        }
    } else {
        rateNote.classList.add('hidden');
    }

    results.classList.remove('hidden');
}

// Auto-init when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
} else {
    init();
}

// Re-init on async page navigation
document.addEventListener('async:load', init);

export {};
