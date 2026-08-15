import av from '../../lib/av';
import { init as initI18n } from '../../lib/profile/i18n';
import { initListEditor } from '../../lib/profile/listEditor';

// Opening the per-row preferences dialog, and the two ways out of it.
//
// The dialog's fields belong to the section's settings form — a <dialog>
// moves to the top layer visually but stays where it is in the DOM — so
// "apply" is just submitting that form, and "cancel" has to put back what
// the fields held when the dialog opened. Without that, closing a dialog
// would leave staged changes that the next Save would write.
function initPrefsDialogs(root) {
    let snapshot = null;

    function fieldsOf(dialog) {
        return Array.from(dialog.querySelectorAll('input[type="checkbox"], select'));
    }

    function take(dialog) {
        return fieldsOf(dialog).map(el => (el.type === 'checkbox' ? el.checked : el.value));
    }

    function restore(dialog, values) {
        fieldsOf(dialog).forEach((el, i) => {
            if (el.type === 'checkbox') el.checked = values[i];
            else el.value = values[i];
        });
    }

    root.addEventListener('click', (e) => {
        const open = e.target.closest('.prefs-subscription');
        if (open) {
            const dialog = document.getElementById(open.getAttribute('data-dialog'));
            if (!dialog) return;
            snapshot = { dialog, values: take(dialog) };
            dialog.showModal();
            return;
        }

        const cancel = e.target.closest('[data-prefs-cancel]');
        if (cancel) {
            const dialog = cancel.closest('dialog');
            if (!dialog) return;
            if (snapshot && snapshot.dialog === dialog) restore(dialog, snapshot.values);
            dialog.close();
            return;
        }

        const apply = e.target.closest('[data-prefs-apply]');
        if (apply) {
            const dialog = apply.closest('dialog');
            const form = dialog?.closest('form');
            dialog?.close();
            snapshot = null;
            // requestSubmit, not submit: the form is data-async, and only
            // requestSubmit runs the handlers that make it so.
            form?.requestSubmit();
        }
    });

    // Escape closes a dialog without a click, so the restore has to happen
    // there too.
    root.addEventListener('cancel', (e) => {
        const dialog = e.target;
        if (snapshot && snapshot.dialog === dialog) restore(dialog, snapshot.values);
    }, true);
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
