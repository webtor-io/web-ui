import av from '../../lib/av';
import DragDrop from '../../lib/dragAndDrop';
import DeleteFromList from '../../lib/deleteFromList';

av(async function(){
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
        confirmMessage: (name) => `Are you sure you want to delete this ${name} backend?`,
        umamiEventName: 'streaming-backend-delete'
    });
});

export {}