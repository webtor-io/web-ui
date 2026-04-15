import av from '../../lib/av';
import DragDrop from '../../lib/dragAndDrop';
import DeleteFromList from '../../lib/deleteFromList';
import { init as initI18n, t, tf } from '../../lib/profile/i18n';

av(async function(){
    await initI18n();
    // Initialize drag and drop functionality for streaming backends
    new DragDrop({
        listSelector: '#backend-list',
        itemSelector: '.backend-item',
        orderInputSelector: '#backend_order',
        dataAttribute: 'data-backend-id'
    });

    // Initialize delete functionality for streaming backends
    new DeleteFromList({
        listSelector: '#backend-list',
        itemSelector: '.backend-item',
        deleteButtonSelector: '.delete-backend',
        dataAttribute: 'data-backend-id',
        deletedInputId: 'deleted_backends',
        orderInputId: 'backend_order',
        emptyStateId: 'backend-empty-state',
        itemNameSelector: '.text-lg.font-medium',
        confirmMessage: (name) => tf('profile.backends.deleteConfirm', name),
        umamiEventName: 'streaming-backend-delete',
        toastMessage: t('profile.backends.deleted')
    });
});

export {}