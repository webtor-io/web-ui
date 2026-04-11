// Discover page state reducer

export const TYPE_PRIORITY = ['movie', 'series'];

export function sortByPriority(types, priority) {
    return [...types].sort((a, b) => {
        const ai = priority.indexOf(a);
        const bi = priority.indexOf(b);
        if (ai !== -1 && bi !== -1) return ai - bi;
        if (ai !== -1) return -1;
        if (bi !== -1) return 1;
        return a.localeCompare(b);
    });
}

export function buildCatalogs(manifests) {
    const catalogs = [];
    for (const m of manifests) {
        for (const cat of (m.manifest.catalogs || [])) {
            catalogs.push({
                id: cat.id,
                type: cat.type,
                name: cat.name || cat.id,
                addonName: m.manifest.name || 'Unknown',
                baseUrl: m.baseUrl,
            });
        }
    }
    return catalogs;
}

export function getCatalogsForType(catalogs, type) {
    return catalogs.filter(c => c.type === type);
}

export function getTypes(catalogs) {
    const types = [...new Set(catalogs.map(c => c.type))];
    return sortByPriority(types, TYPE_PRIORITY);
}

export function getSearchTypes(searchResults) {
    const types = [...new Set(searchResults.map(r => r.type))];
    return sortByPriority(types, TYPE_PRIORITY);
}

export function getSearchResultsForType(searchResults, type) {
    return searchResults.filter(item => item.type === type);
}

// AI recommendations slice. Kept flat inside the main discover state so the
// existing reducer switch can handle it without restructuring the whole app.
// See components/ai/AISection.jsx for the UI that reads this slice.
//
// phase transitions:
//   disabled         — feature flag off (404 from /discover/ai/chips)
//   idle             — not yet loaded
//   loadingChips     — chips request in flight
//   chipsReady       — chips visible, awaiting user input
//   chipsError       — chips load failed
//   streamingClaude  — request submitted; waiting for Claude (slow phase)
//   streamingResolve — Claude done; resolver pushing items live
//   recsReady        — stream finished, all items in
//   recsError        — stream failed
//   quotaExceeded    — last call bounced with 402; UI shows upgrade CTA
export const initialAIState = {
    phase: 'idle',
    chips: [],
    chipsGeneratedAt: 0,
    currentQuery: '',
    recommendations: [],
    // recsExpanded controls whether the grid shows the full set of
    // recommendations or only the first AI_RECS_INITIAL_VISIBLE entries
    // gated behind a "Show more" button. Reset on every new submit so each
    // query starts collapsed again.
    recsExpanded: false,
    // streamExpected is set when the resolver phase begins so the UI can
    // size its loading skeleton. May be larger than the final list because
    // some items get filtered (already-watched, non-IMDB).
    streamExpected: 0,
    // Conversation history for /refine — list of {role, content} pairs,
    // mirroring the server's rec.Message shape.
    conversation: [],
    remainingQuota: null,   // -1 when unknown, null before first load, int otherwise
    dailyQuota: null,       // per-day cap for the user's tier; null until first load
    upgradeQuota: null,     // paid tier daily cap; populated only on quota_exceeded for free users (the upgrade hint)
    quotaResetAt: null,     // unix-seconds timestamp the quota will roll over; populated only on quota_exceeded
    tier: null,             // 'free' | 'paid' — comes from the server
    error: null,            // { code, message } from AIError, or null
};

// AI_RECS_INITIAL_VISIBLE is how many cards the user sees before the
// "Show more" button. Tuned to two rows of the chessboard layout — enough
// to make the section feel populated, few enough that the rest stays a
// deliberate click away.
export const AI_RECS_INITIAL_VISIBLE = 4;

export const initialState = {
    phase: 'loading', // 'loading' | 'ready' | 'error' | 'no-addons' | 'no-catalogs'
    errorMessage: '',
    manifests: [],
    catalogs: [],
    selectedType: null,
    selectedCatalog: null,
    items: [],
    skip: 0,
    hasMore: true,
    page: 0,
    catalogLoading: false,
    // Search
    isSearchMode: false,
    searchQuery: '',
    searchResults: [],
    searchType: 'all',
    searchLoading: false,
    // Modal
    modal: null, // { view: 'loading'|'streams'|'episodes', title, poster, ... }
    // User statuses — accumulated map of IMDB id → { watched, rating }.
    // Additive-only during a session.
    userStatuses: {},
    // AI recommendations slice
    ai: initialAIState,
};

export function discoverReducer(state, action) {
    switch (action.type) {
        case 'INIT_SUCCESS': {
            const { manifests, catalogs, selectedType, selectedCatalog } = action;
            return { ...state, phase: 'ready', manifests, catalogs, selectedType, selectedCatalog };
        }
        case 'INIT_ERROR':
            return { ...state, phase: 'error', errorMessage: action.message };
        case 'SET_PHASE':
            return { ...state, phase: action.phase, errorMessage: action.message || '' };
        case 'SELECT_TYPE': {
            const selectedCatalog = getCatalogsForType(state.catalogs, action.selectedType)[0] || null;
            return { ...state, selectedType: action.selectedType, selectedCatalog, items: [], skip: 0, hasMore: true, page: 0 };
        }
        case 'SELECT_CATALOG':
            return { ...state, selectedCatalog: action.catalog, items: [], skip: 0, hasMore: true, page: 0 };
        case 'CATALOG_LOADING':
            return { ...state, catalogLoading: true };
        case 'CATALOG_LOADED': {
            const newItems = action.append ? [...state.items, ...action.items] : action.items;
            const newPage = action.append ? state.page + 1 : 0;
            return { ...state, catalogLoading: false, items: newItems, hasMore: action.hasMore, skip: newItems.length, page: newPage };
        }
        case 'CATALOG_ERROR':
            return state.items.length > 0
                ? { ...state, catalogLoading: false }
                : { ...state, catalogLoading: false, phase: 'error', errorMessage: action.message };
        case 'SEARCH_START':
            return { ...state, isSearchMode: true, searchQuery: action.query, searchResults: [], searchLoading: true, searchType: 'all' };
        case 'SEARCH_RESULTS':
            return { ...state, searchLoading: false, searchResults: action.results };
        case 'SELECT_SEARCH_TYPE':
            return { ...state, searchType: action.searchType };
        case 'EXIT_SEARCH': {
            const types = getTypes(state.catalogs);
            const selectedType = types[0] || null;
            const selectedCatalog = selectedType ? getCatalogsForType(state.catalogs, selectedType)[0] : null;
            return {
                ...state,
                isSearchMode: false, searchQuery: '', searchResults: [], searchType: 'all', searchLoading: false,
                selectedType, selectedCatalog, items: [], skip: 0, hasMore: true, page: 0,
            };
        }
        case 'SHOW_MODAL':
            return { ...state, modal: action.modal };
        case 'CLOSE_MODAL':
            return { ...state, modal: null };
        case 'USER_STATUSES_MERGED': {
            if (!action.statuses || Object.keys(action.statuses).length === 0) return state;
            return { ...state, userStatuses: { ...state.userStatuses, ...action.statuses } };
        }

        // --- AI recommendations slice ---
        case 'AI_DISABLED':
            return { ...state, ai: { ...state.ai, phase: 'disabled' } };
        case 'AI_LOAD_CHIPS_START':
            return { ...state, ai: { ...state.ai, phase: 'loadingChips', error: null } };
        case 'AI_LOAD_CHIPS_SUCCESS':
            return { ...state, ai: {
                ...state.ai,
                phase: 'chipsReady',
                chips: action.chips || [],
                chipsGeneratedAt: action.generatedAt || 0,
                tier: action.tier || state.ai.tier,
                remainingQuota: action.remainingQuota ?? state.ai.remainingQuota,
                dailyQuota: action.dailyQuota ?? state.ai.dailyQuota,
                error: null,
            } };
        case 'AI_LOAD_CHIPS_ERROR':
            return { ...state, ai: { ...state.ai, phase: 'chipsError', error: action.error } };

        // --- Streaming recommend / refine ---
        // AI_STREAM_START: user submitted a query, EventSource just opened.
        // We clear stale state immediately — both for initial recommend
        // and for refine. Keeping old cards visible during a refine feels
        // misleading: the user just clicked "Remix" and is staring at the
        // PREVIOUS list, which suggests nothing happened. Clearing makes
        // it obvious that work is in progress.
        case 'AI_STREAM_START':
            return { ...state, ai: {
                ...state.ai,
                phase: 'streamingClaude',
                currentQuery: action.displayQuery || action.query,
                streamExpected: 0,
                error: null,
                recommendations: [],
                recsExpanded: false,
            } };
        // AI_STREAM_PHASE: backend transitioned between pipeline stages.
        // We map the server's phase string onto our local UI phase enum,
        // and clear the placeholder list when the resolver actually starts.
        case 'AI_STREAM_PHASE': {
            const next = action.phase === 'resolving' ? 'streamingResolve' : 'streamingClaude';
            return { ...state, ai: {
                ...state.ai,
                phase: next,
                streamExpected: action.expected || state.ai.streamExpected,
                // First time we hit the resolver phase, drop any leftover
                // recommendations from a previous round (initial recommend
                // already cleared them, refine kept them — now reset).
                recommendations: action.phase === 'resolving' ? [] : state.ai.recommendations,
            } };
        }
        // AI_STREAM_ITEM: a single resolved card arrived. Append in order.
        case 'AI_STREAM_ITEM':
            return { ...state, ai: {
                ...state.ai,
                recommendations: [...state.ai.recommendations, action.item],
            } };
        // AI_STREAM_DONE: terminal success. Hydrate quota state and append
        // a synthetic conversation turn so the next refine can ground on
        // what we just showed.
        case 'AI_STREAM_DONE':
            return { ...state, ai: {
                ...state.ai,
                phase: 'recsReady',
                remainingQuota: action.remainingQuota ?? state.ai.remainingQuota,
                dailyQuota: action.dailyQuota ?? state.ai.dailyQuota,
                tier: action.tier || state.ai.tier,
                streamExpected: 0,
                conversation: [
                    ...state.ai.conversation,
                    { role: 'user', content: action.query },
                    { role: 'assistant', content: state.ai.recommendations.map(i => i.title).join(', ') },
                ],
            } };
        // AI_STREAM_ERROR: terminal failure. The 402 (quota exceeded) case
        // is handled by AI_QUOTA_EXCEEDED below; everything else lands here.
        case 'AI_STREAM_ERROR':
            return { ...state, ai: {
                ...state.ai,
                phase: 'recsError',
                error: action.error,
                streamExpected: 0,
            } };
        // AI_EXPAND_RECS: user clicked the "Show more" button to reveal
        // the cards beyond the initial visible window. One-way switch —
        // there's no "collapse" affordance in the UI.
        case 'AI_EXPAND_RECS':
            return { ...state, ai: { ...state.ai, recsExpanded: true } };
        case 'AI_QUOTA_EXCEEDED':
            return { ...state, ai: {
                ...state.ai,
                phase: 'quotaExceeded',
                remainingQuota: 0,
                dailyQuota: action.dailyQuota ?? state.ai.dailyQuota,
                upgradeQuota: action.upgradeQuota ?? state.ai.upgradeQuota,
                quotaResetAt: action.quotaResetAt ?? state.ai.quotaResetAt,
                tier: action.tier || state.ai.tier,
                error: null,
            } };
        case 'AI_RESET':
            return { ...state, ai: {
                ...state.ai,
                phase: state.ai.chips.length > 0 ? 'chipsReady' : 'idle',
                currentQuery: '',
                recommendations: [],
                conversation: [],
                error: null,
            } };

        default:
            return state;
    }
}
