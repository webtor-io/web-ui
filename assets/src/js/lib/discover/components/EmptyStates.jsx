import { useRef, useEffect, useState, useCallback } from 'preact/hooks';
import { rebindAsync } from '../../async';
import { t, tf, langPath } from '../i18n';

export function LoadMore({ onLoadMore }) {
    return (
        <div class="text-center mt-8">
            <button class="btn btn-ghost border border-w-line btn-sm px-8" onClick={onLoadMore}>
                {t('discover.loadMore')}
            </button>
        </div>
    );
}

export function LoadingSpinner() {
    return (
        <div class="text-center py-16">
            <span class="loading loading-spinner loading-lg text-w-cyan"></span>
            <p class="text-w-sub mt-4">{t('discover.loadingCatalogs')}</p>
        </div>
    );
}

export function NoAddons() {
    const ref = useRef(null);
    useEffect(() => {
        if (ref.current) rebindAsync(ref.current);
    }, []);
    return (
        <div ref={ref} class="text-center py-16">
            <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1" stroke="currentColor" class="w-16 h-16 text-w-muted/40 mx-auto mb-4">
                <path stroke-linecap="round" stroke-linejoin="round" d="M15.59 14.37a6 6 0 0 1-5.84 7.38v-4.8m5.84-2.58a14.98 14.98 0 0 0 6.16-12.12A14.98 14.98 0 0 0 9.631 8.41m5.96 5.96a14.926 14.926 0 0 1-5.841 2.58m-.119-8.54a6 6 0 0 0-7.381 5.84h4.8m2.581-5.84a14.927 14.927 0 0 0-2.58 5.84m2.699 2.7c-.103.021-.207.041-.311.06a15.09 15.09 0 0 1-2.448-2.448 14.9 14.9 0 0 1 .06-.312m-2.24 2.39a4.493 4.493 0 0 0-1.757 4.306 4.493 4.493 0 0 0 4.306-1.758M16.5 9a1.5 1.5 0 1 1-3 0 1.5 1.5 0 0 1 3 0Z" />
            </svg>
            <p class="text-lg font-semibold text-w-sub mb-2">{t('discover.noAddonsTitle')}</p>
            <p class="text-sm text-w-muted mb-6">{t('discover.noAddonsDesc')}</p>
            <a class="btn btn-soft-cyan btn-sm px-5" href={langPath('/profile')} data-async-target="main">{t('discover.goToProfile')}</a>
        </div>
    );
}

export function NoCatalogs() {
    const ref = useRef(null);
    useEffect(() => {
        if (ref.current) rebindAsync(ref.current);
    }, []);
    return (
        <div ref={ref} class="text-center py-16">
            <p class="text-lg font-semibold text-w-sub mb-2">{t('discover.noCatalogsTitle')}</p>
            <p class="text-sm text-w-muted mb-6">{t('discover.noCatalogsDesc')}</p>
            <a class="btn btn-soft-cyan btn-sm px-5" href={langPath('/profile')} data-async-target="main">{t('discover.goToProfile')}</a>
        </div>
    );
}

export function ErrorState({ message, onRetry }) {
    return (
        <div class="text-center py-16">
            <p class="text-lg font-semibold text-w-sub mb-2">{t('discover.errorTitle')}</p>
            <p class="text-sm text-w-muted mb-6">{message || t('discover.errorDefault')}</p>
            <button class="btn btn-soft-cyan btn-sm px-5" onClick={onRetry}>{t('discover.retry')}</button>
        </div>
    );
}

// Shown in the catalog grid area when the selected catalog belongs to an
// addon that is currently unreachable. Distinct from generic "No items
// found" so users see why nothing loads instead of suspecting Webtor.
export function CatalogUnavailable({ catalog, onRetry }) {
    const [retrying, setRetrying] = useState(false);
    const handleRetry = useCallback(async () => {
        if (retrying || !onRetry) return;
        setRetrying(true);
        try { await onRetry(); } finally { setRetrying(false); }
    }, [onRetry, retrying]);
    return (
        <div class="text-center py-16 max-w-md mx-auto">
            <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1" stroke="currentColor" class="w-16 h-16 text-yellow-400/40 mx-auto mb-4">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z"/>
            </svg>
            <p class="text-lg font-semibold text-w-sub mb-2">{t('discover.catalogTemporarilyUnavailable')}</p>
            <p class="text-sm text-w-muted mb-4">
                {catalog?.addonName
                    ? tf('discover.catalogUnavailableBody', catalog.addonName)
                    : t('discover.catalogUnavailableBodyGeneric')}
            </p>
            <button class="btn btn-soft-cyan btn-sm px-5" onClick={handleRetry} disabled={retrying}>
                {retrying && <span class="loading loading-spinner loading-xs"></span>}
                {t('discover.retry')}
            </button>
        </div>
    );
}

export function NoResults({ query }) {
    return (
        <div class="text-center py-16">
            <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1" stroke="currentColor" class="w-16 h-16 text-w-muted/40 mx-auto mb-4">
                <circle cx="11" cy="11" r="8"></circle>
                <path stroke-linecap="round" d="m21 21-4.3-4.3"></path>
            </svg>
            <p class="text-lg font-semibold text-w-sub mb-2">{t('discover.noResultsTitle')}</p>
            <p class="text-sm text-w-muted mb-6">{tf('discover.noResultsDesc', query)}</p>
        </div>
    );
}
