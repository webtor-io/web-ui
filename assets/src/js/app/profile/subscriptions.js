import av from '../../lib/av';
import { init as initI18n } from '../../lib/profile/i18n';
import { initListEditor } from '../../lib/profile/listEditor';

// The gear opens the row's preferences dialog. Everything else about it is
// plain markup: its own form posts to its own endpoint (the way the Vault
// modal does), and <form method="dialog"> closes it — so cancelling drops
// the edits with the dialog rather than leaving them staged anywhere.
function initPrefsDialogs(root) {
    root.addEventListener('click', (e) => {
        const gear = e.target.closest('.prefs-subscription');
        if (!gear) return;
        document.getElementById(gear.getAttribute('data-dialog'))?.showModal();
    });
}

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

    initPrefsDialogs(this);
});

export {}
