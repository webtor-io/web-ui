import { useRef, useEffect } from 'preact/hooks';
import { rebindAsync } from '../../async';

export function LoadMore({ onLoadMore }) {
    return (
        <div class="text-center mt-8">
            <button class="btn btn-ghost border border-w-line btn-sm px-8" onClick={onLoadMore}>
                Load more
            </button>
        </div>
    );
}

export function LoadingSpinner() {
    return (
        <div class="text-center py-16">
            <span class="loading loading-spinner loading-lg text-w-cyan"></span>
            <p class="text-w-sub mt-4">Loading catalogs...</p>
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
            <p class="text-lg font-semibold text-w-sub mb-2">No addons configured</p>
            <p class="text-sm text-w-muted mb-6">Add Stremio addons in your profile to start discovering content.</p>
            <a class="btn btn-soft-cyan btn-sm px-5" href="/profile" data-async-target="main">Go to Profile</a>
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
            <p class="text-lg font-semibold text-w-sub mb-2">No catalogs available</p>
            <p class="text-sm text-w-muted mb-6">Your addons don't provide any catalogs. Try adding a catalog addon like Cinemeta.</p>
            <a class="btn btn-soft-cyan btn-sm px-5" href="/profile" data-async-target="main">Go to Profile</a>
        </div>
    );
}

export function ErrorState({ message, onRetry }) {
    return (
        <div class="text-center py-16">
            <p class="text-lg font-semibold text-w-sub mb-2">Something went wrong</p>
            <p class="text-sm text-w-muted mb-6">{message || 'Could not load content.'}</p>
            <button class="btn btn-soft-cyan btn-sm px-5" onClick={onRetry}>Retry</button>
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
            <p class="text-lg font-semibold text-w-sub mb-2">No results found</p>
            <p class="text-sm text-w-muted mb-6">No results found for "{query}". Try a different search term.</p>
        </div>
    );
}
