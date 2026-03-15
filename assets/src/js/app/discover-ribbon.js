import av from '../lib/av';
import { rebindAsync } from '../lib/async';
import { CINEMETA_BASE } from '../lib/discover/client';

const CATALOG_URL = `${CINEMETA_BASE}/catalog/movie/top.json`;
const CARD_COUNT = 7;

function esc(s) {
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function renderCard(item) {
    const poster = item.poster || '';
    const name = item.name || 'Unknown';
    const year = item.releaseInfo || item.year || '';
    const type = item.type || 'movie';
    const href = '/discover?id=' + encodeURIComponent(item.id) + '&type=' + encodeURIComponent(type);

    const a = document.createElement('a');
    a.href = href;
    a.setAttribute('data-async-target', 'main');
    a.setAttribute('data-umami-event', 'discover-ribbon-click');
    a.className = 'shrink-0 w-[140px] group cursor-pointer';
    a.innerHTML =
        '<div class="aspect-[2/3] rounded-xl overflow-hidden border border-w-line group-hover:border-w-cyan/30 group-hover:shadow-[0_0_20px_rgba(0,206,201,0.1)] transition-all duration-300">' +
            (poster
                ? '<img src="' + esc(poster) + '" alt="' + esc(name) + '" class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300" loading="lazy" />'
                : '<div class="w-full h-full bg-gradient-to-br from-w-purple/20 via-w-pink/10 to-w-cyan/15 flex items-center justify-center"><div class="text-center font-bold text-sm p-2 text-w-purpleL/60 line-clamp-3">' + esc(name) + '</div></div>') +
        '</div>' +
        '<p class="mt-2 text-sm font-medium text-w-text truncate group-hover:text-w-cyan transition-colors">' + esc(name) + '</p>' +
        (year ? '<p class="text-xs text-w-muted">' + esc(year) + '</p>' : '');

    return a;
}

av(async function () {
    const container = this.querySelector('#discover-ribbon-cards');
    if (!container) return;

    try {
        const res = await fetch(CATALOG_URL);
        if (!res.ok) return;
        const data = await res.json();
        const items = (data.metas || []).slice(0, CARD_COUNT);
        if (!items.length) return;

        container.innerHTML = '';
        for (const item of items) {
            container.appendChild(renderCard(item));
        }
        rebindAsync(container);
    } catch (e) {
        // On error keep skeleton — not critical
    }
});

export {};
