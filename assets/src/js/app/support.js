import av from '../lib/av';

// data-required marks a field mandatory whenever shown; data-required-select
// narrows that to the listed causes (the infohash is shown for questions too,
// but only complaints cannot do without it — mirrored server-side in
// handlers/support needsInfohash).
function setRequired(input, cause) {
    if (input.getAttribute('data-required') === null) return;
    const only = input.getAttribute('data-required-select');
    if (only && !only.split(',').includes(cause)) {
        input.removeAttribute('required');
        return;
    }
    input.setAttribute('required', 'required');
}

function updateForm(select, inputs, actions) {
    if (select.value === '-1') {
        for (const i of inputs) i.classList.add('hidden');
        actions.classList.add('hidden');
    } else {
        for (const i of inputs) {
            const ds = i.getAttribute('data-select');
            if (!ds) {
                i.classList.remove('hidden');
                setRequired(i, select.value);
            } else if (ds.split(',').includes(select.value)) {
                i.classList.remove('hidden');
                setRequired(i, select.value);
            } else {
                i.classList.add('hidden');
                i.removeAttribute('required');
            }
        }
        actions.classList.remove('hidden');
    }
}

function renderTurnstile(container) {
    const el = container.querySelector('.cf-turnstile');
    if (!el) return;
    const sitekey = el.dataset.sitekey;
    if (!sitekey) return;
    function doRender() {
        if (typeof turnstile !== 'undefined') {
            turnstile.render(el, { sitekey });
        } else {
            setTimeout(doRender, 100);
        }
    }
    doRender();
}

av(async function() {
    const form = this.querySelector('form');
    const select = form.querySelector('select');
    const inputs = form.querySelectorAll('input, textarea');
    const actions = form.querySelector('[data-support-actions]');
    updateForm(select, inputs, actions);
    select.addEventListener('change', () => {
        updateForm(select, inputs, actions);
    });
    renderTurnstile(form);
});

export {}