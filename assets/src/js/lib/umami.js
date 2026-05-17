export function init(window, config) {
    const {
        screen: { width, height },
        navigator: { language },
        location,
        localStorage,
        document,
        history,
    } = window;
    const { hostname, href } = location;
    let { referrer } = document;

    if (config.referrer) {
        referrer = config.referrer;
    }

    const _data = 'data-';
    const website = config.website_id;
    const hostUrl = config.host_url;
    const tag = config.tag;
    const autoTrack = config.auto_track !== false;
    const excludeSearch = config.exclude_search === true;
    const domain = config.domains || '';
    const domains = domain.split(',').map(n => n.trim());
    const host = hostUrl;
    const endpoint = `${host.replace(/\/$/, '')}/api/send`;
    const screen = `${width}x${height}`;
    const eventRegex = /data-umami-event-([\w-_]+)/;
    const eventNameAttribute = _data + 'umami-event';
    const delayDuration = 300;

    /* Helper functions */

    const encode = str => {
        if (!str) {
            return undefined;
        }

        try {
            const result = decodeURI(str);

            if (result !== str) {
                return result;
            }
        } catch (e) {
            return str;
        }

        return encodeURI(str);
    };

    const parseURL = url => {
        try {
            const { pathname, search } = new URL(url);
            url = pathname + search;
        } catch (e) {
            /* empty */
        }
        return excludeSearch ? url.split('?')[0] : url;
    };

    const getPayload = () => ({
        website,
        hostname,
        screen,
        language,
        title: encode(title),
        url: encode(currentUrl),
        referrer: encode(currentRef),
        tag: tag ? tag : undefined,
    });

    /* Event handlers */

    const handlePush = (state, title, url) => {
        if (!url) return;

        currentRef = currentUrl;
        currentUrl = parseURL(url.toString());

        if (currentUrl !== currentRef) {
            setTimeout(track, delayDuration);
        }
    };

    const handlePathChanges = () => {
        const hook = (_this, method, callback) => {
            const orig = _this[method];

            return (...args) => {
                callback.apply(null, args);

                return orig.apply(_this, args);
            };
        };

        history.pushState = hook(history, 'pushState', handlePush);
        history.replaceState = hook(history, 'replaceState', handlePush);
    };

    const handleTitleChanges = () => {
        const observer = new MutationObserver(([entry]) => {
            title = entry && entry.target ? entry.target.text : undefined;
        });

        const node = document.querySelector('head > title');

        if (node) {
            observer.observe(node, {
                subtree: true,
                characterData: true,
                childList: true,
            });
        }
    };

    const handleClicks = () => {
        document.addEventListener(
            'click',
            async e => {
                const isSpecialTag = tagName => ['BUTTON', 'A', 'LABEL'].includes(tagName);

                const trackElement = async el => {
                    const attr = el.getAttribute.bind(el);
                    const eventName = attr(eventNameAttribute);

                    if (eventName) {
                        const eventData = {};

                        el.getAttributeNames().forEach(name => {
                            const match = name.match(eventRegex);

                            if (match) {
                                eventData[match[1]] = attr(name);
                            }
                        });

                        return track(eventName, eventData);
                    }
                };

                const findParentTag = (rootElem, maxSearchDepth) => {
                    let currentElement = rootElem;
                    for (let i = 0; i < maxSearchDepth; i++) {
                        if (isSpecialTag(currentElement.tagName)) {
                            return currentElement;
                        }
                        currentElement = currentElement.parentElement;
                        if (!currentElement) {
                            return null;
                        }
                    }
                };

                const el = e.target;
                const parentElement = isSpecialTag(el.tagName) ? el : findParentTag(el, 10);

                if (parentElement) {
                    const { href, target } = parentElement;
                    const eventName = parentElement.getAttribute(eventNameAttribute);

                    if (eventName) {
                        if (parentElement.tagName === 'A') {
                            const external =
                                target === '_blank' ||
                                e.ctrlKey ||
                                e.shiftKey ||
                                e.metaKey ||
                                (e.button && e.button === 1) ||
                                parentElement.hasAttribute('download');

                            if (eventName && href) {
                                if (!external) {
                                    e.preventDefault();
                                }
                                return trackElement(parentElement).then(() => {
                                    // if (!external) location.href = href;
                                });
                            }
                        } else if (parentElement.tagName === 'BUTTON') {
                            return trackElement(parentElement);
                        } else if (parentElement.tagName === 'LABEL') {
                            return trackElement(parentElement);
                        }
                    }
                } else {
                    return trackElement(el);
                }
            },
            true,
        );
    };

    /* Tracking functions */

    const trackingDisabled = () =>
        !website ||
        (localStorage && localStorage.getItem('umami.disabled')) ||
        (domain && !domains.includes(hostname));

    const send = async (payload, type = 'event') => {
        if (trackingDisabled()) return;

        const headers = {
            'Content-Type': 'application/json',
        };

        if (typeof cache !== 'undefined') {
            headers['x-umami-cache'] = cache;
        }

        try {
            const res = await fetch(endpoint, {
                method: 'POST',
                body: JSON.stringify({ type, payload }),
                headers,
            });
            const text = await res.text();

            return (cache = text);
        } catch (e) {
            /* empty */
        }
    };

    const init = () => {
        if (!initialized) {
            track();
            handlePathChanges();
            handleTitleChanges();
            handleClicks();
            initialized = true;
        }
    };

    const resolveDefaults = () => {
        try {
            const d = config.defaultData;
            if (typeof d === 'function') return d() || {};
            if (d && typeof d === 'object') return d;
        } catch (e) { /* ignore */ }
        return {};
    };

    const mergeData = data => {
        const defaults = resolveDefaults();
        if (data && typeof data === 'object') return { ...defaults, ...data };
        return Object.keys(defaults).length ? defaults : undefined;
    };

    const track = (obj, data) => {
        if (typeof obj === 'string') {
            return send({
                ...getPayload(),
                name: obj,
                data: mergeData(data),
            });
        } else if (typeof obj === 'object') {
            return send(obj);
        } else if (typeof obj === 'function') {
            return send(obj(getPayload()));
        }
        return send(getPayload());
    };

    // UUID v5 (SHA-1, URL namespace) of `name`. We hash whatever raw id the
    // caller passes (server session cookie value, supertokens userId, anything)
    // into a 36-char UUID before handing it to Umami: the server-side validator
    // silently rejects anything that doesn't match the UUID format and falls
    // back to its own anonymous cookie id, which is what masked our anon→paid
    // attribution for months. Stable: same raw id → same UUID forever.
    const NAMESPACE_URL = new Uint8Array([
        0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1,
        0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8,
    ]);
    const uuidV5 = async name => {
        if (!name || !window.crypto || !window.crypto.subtle) return null;
        const nameBytes = new TextEncoder().encode(name);
        const buf = new Uint8Array(NAMESPACE_URL.length + nameBytes.length);
        buf.set(NAMESPACE_URL, 0);
        buf.set(nameBytes, NAMESPACE_URL.length);
        const d = new Uint8Array(await window.crypto.subtle.digest('SHA-1', buf));
        d[6] = (d[6] & 0x0f) | 0x50; // version 5
        d[8] = (d[8] & 0x3f) | 0x80; // RFC 4122 variant
        const hex = Array.from(d.slice(0, 16)).map(b => b.toString(16).padStart(2, '0')).join('');
        return hex.slice(0, 8) + '-' + hex.slice(8, 12) + '-' + hex.slice(12, 16) + '-' + hex.slice(16, 20) + '-' + hex.slice(20, 32);
    };

    const identify = async (idOrData, maybeData) => {
        let rawId;
        let data;
        if (typeof idOrData === 'string') {
            rawId = idOrData;
            data = (maybeData && typeof maybeData === 'object') ? maybeData : undefined;
        } else if (idOrData && typeof idOrData === 'object') {
            data = idOrData;
        }
        const payload = { ...getPayload() };
        if (rawId) {
            const id = await uuidV5(rawId);
            if (id) payload.id = id;
        }
        const merged = mergeData(data);
        if (merged) payload.data = merged;
        return send(payload, 'identify');
    };

    /* Start */

    if (!window.umami) {
        window.umami = {
            track,
            identify,
        };
    }

    let currentUrl = parseURL(href);
    let currentRef = referrer !== hostname ? referrer : '';
    let title = document.title;
    let cache;
    let initialized;

    if (autoTrack && !trackingDisabled()) {
        if (document.readyState === 'complete') {
            init();
        } else {
            document.addEventListener('readystatechange', init, true);
        }
    }
    return window.umami;
}