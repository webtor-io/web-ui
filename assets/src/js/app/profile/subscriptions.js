import av from '../../lib/av';
import { init as initI18n } from '../../lib/profile/i18n';
import { initListEditor } from '../../lib/profile/listEditor';

av(async function(){
    await initI18n();

    // Same staged-delete list as the addon and indexer sections, minus the
    // two things subscriptions have no use for: there is no order between
    // them (rows are not draggable, so the shared drag binding stays inert)
    // and nothing to re-probe.
    initListEditor({
        kind: 'subscription',
        i18nPrefix: 'profile.subscriptions',
        umamiDeleteEvent: 'subscription-delete',
    });
});

export {}
