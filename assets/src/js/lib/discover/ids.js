// Shared id guards for the discover JSON endpoints.

// bareImdbTitleID accepts bare IMDB title ids (tt*) only — the one id
// shape whose type the server enrichment pipeline can verify. Episode
// ids carry ':' and bare tmdb* ids are namespace-ambiguous; both are
// rejected server-side too (handlers/discover/localize.go, reviews.go),
// this is the client-side mirror used by localizeClient and
// reviewsClient.
export function bareImdbTitleID(id) {
    return typeof id === 'string' && id.startsWith('tt') && !id.includes(':');
}
