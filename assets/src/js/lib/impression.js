// Impression events from markup, the way click events already are.
//
// Clicks ride on data-umami-event and Umami wires them itself; there is no
// equivalent for "this offer was rendered", which is why several surfaces had
// clicks and no denominator. An element declares
//
//     data-umami-impression="release-sub-seen"
//     data-umami-impression-source="resource_banner"
//
// and every data-umami-impression-* attribute becomes a property. A module
// that owns the fragment calls trackImpressions(root) after render.
//
// Deduped per page load on (event, props): async islands re-render on every
// action (subscribe flips the banner), and a re-render is not a second
// impression. A genuinely different offer — another source, another id —
// has different props and counts.

const PREFIX = 'umamiImpression';

export function impressionFromDataset(dataset) {
    const name = dataset[PREFIX];
    const props = {};
    for (const [k, v] of Object.entries(dataset)) {
        if (k === PREFIX || !k.startsWith(PREFIX)) continue;
        const rest = k.slice(PREFIX.length);
        props[rest.charAt(0).toLowerCase() + rest.slice(1)] = v;
    }
    return { name, props };
}

export function impressionKey(name, props) {
    return name + '|' + Object.keys(props).sort().map((k) => k + '=' + props[k]).join('&');
}

const seen = new Set();

export function trackImpressions(root, umami = (typeof window !== 'undefined' ? window.umami : undefined), registry = seen) {
    if (!root || !umami) return 0;
    const els = [];
    if (root.matches && root.matches('[data-umami-impression]')) els.push(root);
    if (root.querySelectorAll) els.push(...root.querySelectorAll('[data-umami-impression]'));
    let sent = 0;
    for (const el of els) {
        const { name, props } = impressionFromDataset(el.dataset);
        if (!name) continue;
        const key = impressionKey(name, props);
        if (registry.has(key)) continue;
        registry.add(key);
        umami.track(name, props);
        sent++;
    }
    return sent;
}
