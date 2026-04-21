import av from '../lib/av';

function setRequired(input) {
    if (input.getAttribute('data-required') !== null)  {
        input.setAttribute('required', 'required');
    }
}

function updateForm(select, inputs, submit) {
    if (select.value === '-1') {
        for (const i of inputs) i.classList.add('hidden');
        submit.classList.add('hidden');
    } else {
        for (const i of inputs) {
            const ds = i.getAttribute('data-select');
            if (!ds) {
                i.classList.remove('hidden');
                setRequired(i);
            } else if (ds.split(',').includes(select.value)) {
                i.classList.remove('hidden');
                setRequired(i);
            } else {
                i.classList.add('hidden');
                i.removeAttribute('required');
            }
        }
        submit.classList.remove('hidden');
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
    const submit = form.querySelector('button');
    updateForm(select, inputs, submit);
    select.addEventListener('change', () => {
        updateForm(select, inputs, submit);
    });
    renderTurnstile(form);
});

export {}