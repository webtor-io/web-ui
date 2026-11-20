const TOAST_DURATION = 3000;
const CONTAINER_ID = 'toast-container';

function getContainer() {
    return document.getElementById(CONTAINER_ID);
}

function show(msg, type, duration = TOAST_DURATION) {
    const container = getContainer();
    if (!container) return;

    const alert = document.createElement('div');
    alert.className = `toast-alert toast-alert-${type}`;
    alert.setAttribute('role', 'alert');
    alert.textContent = msg;

    container.appendChild(alert);

    // Trigger slide-in
    requestAnimationFrame(() => {
        alert.classList.add('toast-show');
    });

    // Auto-dismiss
    setTimeout(() => {
        alert.classList.remove('toast-show');
        alert.addEventListener('transitionend', () => alert.remove(), { once: true });
        // Fallback removal in case transitionend doesn't fire
        setTimeout(() => alert.remove(), 300);
    }, duration);
}

const toast = {
    success(msg, duration) { show(msg, 'success', duration); },
    error(msg, duration) { show(msg, 'error', duration); },
    info(msg, duration) { show(msg, 'info', duration); },
};

window.toast = toast;

export default toast;
