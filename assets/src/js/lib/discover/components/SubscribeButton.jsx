import { useCallback } from 'preact/hooks';
import { t, tf } from '../i18n';
import { subscriptionKey } from '../subscriptionsClient';

// The bell. One component for every subscribe affordance in the modal so
// the two states — offering and following — never drift apart between the
// season row and the empty states.
//
// `target` is {kind, videoId, season, source}; `subscriptionKeys` is the Set
// DiscoverApp keeps. Rendering nothing when there is no target is deliberate:
// a library item or a series opened without an episode has nothing a poller
// could search for.
export function SubscribeButton({ target, subscriptionKeys, onToggle, size = 'sm' }) {
    const handle = useCallback((e) => {
        e?.stopPropagation?.();
        if (onToggle && target) onToggle(target);
    }, [onToggle, target]);

    if (!target || !target.videoId) return null;

    const key = subscriptionKey(target.kind, target.videoId, target.season);
    const subscribed = !!(subscriptionKeys && subscriptionKeys.has && subscriptionKeys.has(key));

    const label = subscribed ? t('discover.subscriptions.unsubscribe') : t('discover.subscriptions.subscribe');
    const title = target.kind === 'season'
        ? tf('discover.subscriptions.seasonTitle', target.season)
        : label;

    return (
        <button
            type="button"
            title={title}
            aria-pressed={subscribed}
            onClick={handle}
            class={[
                'btn',
                size === 'xs' ? 'btn-xs' : 'btn-sm',
                subscribed ? 'btn-ghost border border-w-line text-w-cyan' : 'btn-soft-cyan',
            ].join(' ')}
        >
            <BellIcon filled={subscribed} />
            {label}
        </button>
    );
}

// OnAirDot marks a season that is still being broadcast. A pulsing dot
// rather than a word: it sits inside a season chip that is already only two
// characters wide.
export function OnAirDot() {
    return (
        <span class="relative inline-flex ml-1.5 h-1.5 w-1.5 align-middle" aria-hidden="true">
            <span class="absolute inline-flex h-full w-full rounded-full bg-w-cyan opacity-60 motion-safe:animate-ping"></span>
            <span class="relative inline-flex h-1.5 w-1.5 rounded-full bg-w-cyan"></span>
        </span>
    );
}

function BellIcon({ filled }) {
    return (
        <svg class="w-4 h-4" viewBox="0 0 24 24" fill={filled ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="1.8">
            <path stroke-linecap="round" stroke-linejoin="round" d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/>
            <path stroke-linecap="round" stroke-linejoin="round" d="M13.73 21a2 2 0 0 1-3.46 0"/>
        </svg>
    );
}
