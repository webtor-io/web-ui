import av from '../../lib/av';
import DragDrop from '../../lib/dragAndDrop';

av(async function(){
    // Initialize drag and drop functionality for streaming backends
    new DragDrop({
        listSelector: '#backend-list',
        itemSelector: '.backend-item',
        orderInputSelector: '#backend_order',
        dataAttribute: 'data-backend-id'
    });

    // Handle delete button clicks
    const deletedBackends = new Set();
    const deletedBackendsInput = document.getElementById('deleted_backends');
    
    function handleDeleteBackend(event) {
        const button = event.target.closest('.delete-backend');
        if (!button) return;
        
        const backendId = button.getAttribute('data-backend-id');
        const backendItem = button.closest('.backend-item');
        const backendName = backendItem.querySelector('.text-lg.font-medium').textContent.trim();
        
        if (confirm(`Are you sure you want to delete this ${backendName} backend?`)) {
            // Track delete event with Umami
            if (window.umami) {
                window.umami.track('streaming-backend-delete');
            }
            
            // Add to deleted backends set
            deletedBackends.add(backendId);
            
            // Update hidden input
            deletedBackendsInput.value = Array.from(deletedBackends).join(',');
            
            // Remove the element from DOM
            backendItem.remove();
            
            // Update the backend order input to remove deleted backend
            const orderInput = document.getElementById('backend_order');
            const currentOrder = orderInput.value.split(',').filter(id => !deletedBackends.has(id));
            orderInput.value = currentOrder.join(',');
            
            // Show empty state if no backends left
            const backendList = document.getElementById('backend-list');
            const remainingBackends = backendList.querySelectorAll('.backend-item[data-backend-id]');
            if (remainingBackends.length === 0) {
                const emptyState = document.getElementById('backend-empty-state');
                if (emptyState) {
                    emptyState.classList.remove('hidden');
                }
            }
        }
    }
    
    // Add event listener to the backend list using event delegation
    const backendList = document.getElementById('backend-list');
    if (backendList) {
        backendList.addEventListener('click', handleDeleteBackend);
    }
});

export {}