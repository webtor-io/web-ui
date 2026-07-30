const STORAGE_KEY = 'onboarding-checklist-collapsed';

// Reading or writing localStorage throws where site data is blocked, so both
// are guarded: a storage failure must degrade to "always expanded", never take
// the toggle down with it.
function readCollapsed() {
    try {
        return localStorage.getItem(STORAGE_KEY) === '1';
    } catch (e) {
        return false;
    }
}

function writeCollapsed(collapsed) {
    try {
        if (collapsed) {
            localStorage.setItem(STORAGE_KEY, '1');
        } else {
            localStorage.removeItem(STORAGE_KEY);
        }
    } catch (e) {
        // Collapse still works for this page view; it just won't be remembered.
    }
}

// How many people actually see the card is the question that decides whether
// it deserves more surfaces, so the impression is reported here rather than
// inferred from home-page views: the card renders for a narrow slice of them
// (signed in, account under two weeks old, steps outstanding).
//
// done/total/locked ride along so free and paid impressions stay separable and
// so we can tell "seen at 1/3" from "seen at 3/3".
// Async navigation re-runs init every time the user returns to the home page,
// so without this the same person reports several impressions and inflates the
// denominator for step-click conversion. One per full page load is the number
// we actually want.
let impressionReported = false;

function trackImpression(root) {
    if (impressionReported || !window.umami) {
        return;
    }
    impressionReported = true;
    window.umami.track('onboarding-shown', {
        done: Number(root.dataset.onboardingDone || 0),
        total: Number(root.dataset.onboardingTotal || 0),
        locked: Number(root.dataset.onboardingLocked || 0),
    });
}

// The initial collapsed state is applied by the inline snippet in the partial,
// which runs before paint. This module only wires up the interaction — doing
// the restore here as well would flash the list open on every visit, since the
// asset helper loads it async.
export function initOnboardingChecklist(root) {
    // Before the guard below: the card was seen whether or not the collapse
    // control wired up, and an under-count here would be read as "nobody sees
    // it" — the exact conclusion this metric exists to test.
    trackImpression(root);

    const toggle = root.querySelector('[data-checklist-toggle]');
    const steps = root.querySelector('#onboarding-checklist-steps');
    const chevron = root.querySelector('[data-checklist-chevron]');
    if (!toggle || !steps) {
        return;
    }

    // Belt and braces: if the inline snippet was stripped (CSP, proxy), the
    // button would still be hidden and the card stuck expanded.
    toggle.classList.remove('hidden');

    // Arriving from the navbar counter (the no-JS fallback path): the user asked
    // to see the checklist, so a collapsed card would answer their click with an
    // empty header. Not persisted — they asked for this view, not to change a
    // preference they set deliberately.
    if (window.location.hash === '#onboarding-checklist') {
        steps.style.display = '';
        toggle.setAttribute('aria-expanded', 'true');
        if (chevron) {
            chevron.style.transform = '';
        }
    }

    function apply(collapsed) {
        steps.style.display = collapsed ? 'none' : '';
        toggle.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
        if (chevron) {
            chevron.style.transform = collapsed ? 'rotate(180deg)' : '';
        }
    }

    toggle.addEventListener('click', function() {
        const collapsed = toggle.getAttribute('aria-expanded') === 'true';
        apply(collapsed);
        writeCollapsed(collapsed);
        // Tells us whether people hide the card — a high collapse rate would
        // mean the card is noise, not that it needs more surfaces.
        if (window.umami) {
            window.umami.track(collapsed ? 'onboarding-collapsed' : 'onboarding-expanded');
        }
    });

    // Keep the DOM honest if something else changed storage (another tab).
    // Returned so the caller can unbind: init runs again on every async return
    // to the home page, and window listeners would otherwise pile up, each
    // holding a detached card.
    const onStorage = function(e) {
        if (e.key === STORAGE_KEY) {
            apply(readCollapsed());
        }
    };
    window.addEventListener('storage', onStorage);
    return function destroy() {
        window.removeEventListener('storage', onStorage);
    };
}
