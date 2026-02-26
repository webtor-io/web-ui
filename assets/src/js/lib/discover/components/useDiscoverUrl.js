import { useRef, useEffect, useCallback } from 'preact/hooks';
import { addPopstateFilter } from '../../async';

function buildUrl(params) {
    const url = new URLSearchParams(window.location.search);
    for (const [k, v] of Object.entries(params)) {
        if (v != null && v !== '') url.set(k, String(v));
        else url.delete(k);
    }
    const search = url.toString() ? `?${url}` : '';
    return window.location.pathname + search;
}

function buildUrlClean(params) {
    const url = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
        if (v != null && v !== '') url.set(k, String(v));
    }
    const search = url.toString() ? `?${url}` : '';
    return window.location.pathname + search;
}

export function useDiscoverUrl(pathPrefix) {
    const isPopstate = useRef(false);
    const handlerRef = useRef(null);

    const push = useCallback((params) => {
        const url = buildUrl(params);
        const main = document.querySelector('main');
        window.history.pushState({
            context: 'links', url, targetSelector: 'main',
            layout: main ? main.getAttribute('data-async-layout') : '',
            fetchParams: {},
        }, '', url);
    }, []);

    const replace = useCallback((params) => {
        const existing = window.history.state || {};
        const newUrl = buildUrl(params);
        window.history.replaceState({ ...existing, url: newUrl }, '', newUrl);
    }, []);

    const replaceAll = useCallback((params) => {
        const existing = window.history.state || {};
        const newUrl = buildUrlClean(params);
        window.history.replaceState({ ...existing, url: newUrl }, '', newUrl);
    }, []);

    const withPopstate = useCallback((fn) => {
        isPopstate.current = true;
        try { fn(); } finally { isPopstate.current = false; }
    }, []);

    const onPopstate = useCallback((handler) => {
        handlerRef.current = handler;
    }, []);

    useEffect(() => {
        const removeFilter = addPopstateFilter(() =>
            window.location.pathname.startsWith(pathPrefix)
        );
        const listener = () => {
            if (!window.location.pathname.startsWith(pathPrefix)) return;
            if (handlerRef.current) {
                handlerRef.current(new URLSearchParams(window.location.search));
            }
        };
        window.addEventListener('popstate', listener);
        return () => {
            removeFilter();
            window.removeEventListener('popstate', listener);
        };
    }, [pathPrefix]);

    return { push, replace, replaceAll, isPopstate, withPopstate, onPopstate };
}
