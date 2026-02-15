const TOAST_DURATION = 3000;
const CONTAINER_ID = 'toast-container';

function getContainer() {
    return document.getElementById(CONTAINER_ID);
}

function show(msg, type) {
    const container = getContainer();
    if (!container) return;

    const alert = document.createElement('div');
    alert.className = `toast-alert toast-alert-${type}`;
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
    }, TOAST_DURATION);
}

const toast = {
    success(msg) { show(msg, 'success'); },
    error(msg) { show(msg, 'error'); },
    info(msg) { show(msg, 'info'); },
};

window.toast = toast;

export default toast;
