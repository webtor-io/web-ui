import { useState, useCallback } from 'preact/hooks';
import { t, tf, langPath } from '../i18n';

// Page-level health surface for the user's addons. Hidden when every
// addon is reachable; otherwise renders a single warning row that
// expands into a per-addon list with retry. Lives inside the Discover
// sticky header so it stays visible when scrolling through results.
export function AddonHealthChip({ addons, onRetry }) {
    const [expanded, setExpanded] = useState(false);
    const [retrying, setRetrying] = useState(false);

    const handleRetry = useCallback(async (e) => {
        e.stopPropagation();
        if (retrying || !onRetry) return;
        setRetrying(true);
        try {
            await onRetry();
        } finally {
            setRetrying(false);
        }
    }, [onRetry, retrying]);

    if (!addons || addons.length === 0) return null;
    const failed = addons.filter(a => a.status !== 'ok');
    if (failed.length === 0) return null;

    const allDown = failed.length === addons.length;
    const headline = allDown
        ? t('discover.allAddonsDown')
        : tf('discover.addonsPartialDown', failed.length, addons.length);

    const toggle = () => setExpanded(v => !v);

    return (
        <div class="mb-3 rounded-lg border border-yellow-500/30 bg-yellow-500/5">
            <div class="flex items-center gap-2 px-3 py-2">
                <svg class="w-4 h-4 text-yellow-400 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z"/>
                </svg>
                <button
                    type="button"
                    onClick={toggle}
                    class="flex-1 min-w-0 text-left text-sm text-yellow-100/90 hover:text-yellow-100 truncate"
                    aria-expanded={expanded}
                >
                    {headline}
                </button>
                <button
                    type="button"
                    onClick={handleRetry}
                    disabled={retrying}
                    class="btn btn-ghost btn-xs text-yellow-100/80 hover:bg-yellow-500/10 hover:text-yellow-100"
                >
                    {retrying && <span class="loading loading-spinner loading-xs"></span>}
                    {t('discover.retry')}
                </button>
                <button
                    type="button"
                    onClick={toggle}
                    class="btn btn-ghost btn-xs btn-square text-yellow-100/60"
                    aria-label={expanded ? t('discover.collapse') : t('discover.expand')}
                >
                    <svg class={`w-3.5 h-3.5 transition-transform ${expanded ? 'rotate-180' : ''}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M6 9l6 6 6-6"/>
                    </svg>
                </button>
            </div>
            {expanded && (
                <div class="px-3 pb-3 pt-1 border-t border-yellow-500/20 flex flex-col gap-1.5">
                    {addons.map(a => (
                        <AddonRow key={a.baseUrl} addon={a} />
                    ))}
                    <a
                        href={langPath('/profile')}
                        class="text-xs text-yellow-100/70 hover:text-yellow-100 underline underline-offset-2 mt-1"
                        data-async-target="main"
                    >
                        {t('discover.manageAddons')}
                    </a>
                </div>
            )}
        </div>
    );
}

function AddonRow({ addon }) {
    const { name, host, status, source, capabilities } = addon;
    const isOk = status === 'ok';
    const isMisconfigured = status === 'misconfigured';

    let icon, statusText, textClass;
    if (isOk) {
        icon = (
            <svg class="w-3.5 h-3.5 text-green-400 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                <path d="M20 6L9 17l-5-5"/>
            </svg>
        );
        statusText = capabilities?.length ? capabilities.join(', ') : t('discover.addonReady');
        textClass = 'text-w-sub';
    } else if (isMisconfigured) {
        icon = (
            <svg class="w-3.5 h-3.5 text-red-400 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                <path d="M18 6L6 18M6 6l12 12"/>
            </svg>
        );
        statusText = t('discover.addonMisconfigured');
        textClass = 'text-red-300/80';
    } else {
        icon = (
            <svg class="w-3.5 h-3.5 text-yellow-400 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                <path d="M18 6L6 18M6 6l12 12"/>
            </svg>
        );
        statusText = source === 'cache'
            ? t('discover.addonUnreachableCached')
            : t('discover.addonUnreachable');
        textClass = 'text-yellow-200/80';
    }

    return (
        <div class="flex items-baseline gap-2 text-xs">
            <span class="self-center">{icon}</span>
            <span class="font-medium text-w-text truncate flex-shrink-0 max-w-[40%]">{name}</span>
            <span class="text-w-muted truncate flex-shrink min-w-0">{host}</span>
            <span class={`ml-auto truncate ${textClass} flex-shrink-0`}>{statusText}</span>
        </div>
    );
}
