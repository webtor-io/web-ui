// Shared HTTP plumbing for the discover JSON endpoints. Single source of
// truth for the CSRF/XHR header set — watchlistClient, localizeClient and
// the user-status fetch in discoverUtils all post with these.

export function csrfHeaders() {
    return {
        'Content-Type': 'application/json',
        'Accept': 'application/json',
        'X-Requested-With': 'XMLHttpRequest',
        'X-CSRF-TOKEN': window._CSRF,
    };
}
