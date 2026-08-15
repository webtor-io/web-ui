import av from '../../lib/av';
import { init as initI18n } from '../../lib/profile/i18n';
import { initListEditor } from '../../lib/profile/listEditor';

av(async function(){
    await initI18n();

    initListEditor({
        kind: 'indexer',
        i18nPrefix: 'profile.indexers',
        umamiDeleteEvent: 'torznab-indexer-delete',
        // Re-probes t=caps server-side. Capabilities change whenever the user
        // adds trackers to their Jackett/Prowlarr, and the query shape we send
        // depends on them.
        refreshUrl: id => `/torznab/indexer/${id}/refresh-caps`,
        onRefreshed: (item, data) => {
            const nameEl = item.querySelector('.font-semibold');
            if (nameEl && data.name) nameEl.textContent = data.name;
        },
    });
});

export {}
