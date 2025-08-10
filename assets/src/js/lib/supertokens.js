import SuperTokens from 'supertokens-web-js';
import Session from 'supertokens-web-js/recipe/session';
import Passwordless, { createCode, consumeCode, signOut } from "supertokens-web-js/recipe/passwordless";
import ThirdParty, { getAuthorisationURLWithQueryParamsAndSetState, signInAndUp } from "supertokens-web-js/recipe/thirdparty";

function preAPIHook(csrf) {
    return function(context) {
        let requestInit = context.requestInit;
        let url = context.url;
        let headers = {
            ...requestInit.headers,
            'X-CSRF-TOKEN': csrf,
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

export async function sendMagicLink({email}, csrf) {
    await initSuperTokens(csrf);
    return await createCode({
        email,
        options: {
            preAPIHook: preAPIHook(csrf),
        },
    });
}

export async function handleMagicLinkClicked(csrf) {
    await initSuperTokens(csrf);
    return await consumeCode({
        options: {
            preAPIHook: preAPIHook(csrf),
        },
    });
}

export async function handleCallback(csrf) {
    await initSuperTokens(csrf);
    return await signInAndUp({
        options: {
            preAPIHook: preAPIHook(csrf),
        },
    });
}

export async function logout(csrf) {
    await initSuperTokens(csrf);
    return await signOut({
        options: {
            preAPIHook: preAPIHook(csrf),
        },
    });
}

export async function refresh(csrf) {
    await initSuperTokens(csrf);
    return Session.attemptRefreshingSession();
}

export async function init(csrf) {
    await initSuperTokens(csrf);
}

export async function signInWith(csrf, provider) {
    await initSuperTokens(csrf);
    const redirectUrl = window._domain + "/auth/callback/" + provider;
    const authUrl = await getAuthorisationURLWithQueryParamsAndSetState({
        thirdPartyId: provider,
        frontendRedirectURI: redirectUrl,
        options: {
            preAPIHook: preAPIHook(csrf),
        },
    });
    window.location.assign(authUrl);
}

let inited = false;
async function initSuperTokens(csrf) {
    if (inited) {
        return;
    }
    inited = true;
    SuperTokens.init({
        appInfo: {
            apiDomain:   window._domain,
            apiBasePath: '/auth',
            appName:     'webtor',
        },
        recipeList: [
            Session.init({
                preAPIHook: preAPIHook(csrf),
            }),
            Passwordless.init(),
            ThirdParty.init(),
        ],
    });

}
