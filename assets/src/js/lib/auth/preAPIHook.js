// Its own module so a test can load it: lib/supertokens.js pulls in the SDK,
// which only webpack resolves.
export function preAPIHook(csrf) {
    return function(context) {
        let requestInit = context.requestInit;
        let url = context.url;
        let headers = {
            ...requestInit.headers,
            // At request time, not the init-time copy: the SDK is set up once
            // per page, and a tab that outlived its web-ui session may have
            // taken a fresh token since (useWatchHistory). With the old one
            // such a tab's refresh failed CSRF too, and its saves stayed 401.
            'X-CSRF-TOKEN': window._CSRF || csrf,
        };
        requestInit = {
            ...requestInit,
            headers,
        }
        return {
            requestInit, url
        };
    }
}
