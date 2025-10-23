import av from '../../lib/av';
import DragDrop from '../../lib/dragAndDrop';

av(async function(){
    // Initialize drag and drop functionality for addon URLs
    new DragDrop({
        listSelector: '#addon-list',
        itemSelector: '.addon-item',
        orderInputSelector: '#addon_order',
        dataAttribute: 'data-addon-id'
    });

    // Handle delete button clicks
    const deletedAddons = new Set();
    const deletedAddonsInput = document.getElementById('deleted_addons');
    
    function handleDeleteAddon(event) {
        const button = event.target.closest('.delete-addon');
        if (!button) return;
        
        const addonId = button.getAttribute('data-addon-id');
        const addonItem = button.closest('.addon-item');
        const addonUrl = addonItem.querySelector('.font-medium').textContent.trim();
        
        if (confirm(`Are you sure you want to delete this addon?\n\n${addonUrl}`)) {
            // Track delete event with Umami
            if (window.umami) {
                window.umami.track('addon-url-delete');
            }
            
            // Add to deleted addons set
            deletedAddons.add(addonId);
            
            // Update hidden input
            deletedAddonsInput.value = Array.from(deletedAddons).join(',');
            
            // Remove the element from DOM
            addonItem.remove();
            
            // Update the addon order input to remove deleted addon
            const orderInput = document.getElementById('addon_order');
            const currentOrder = orderInput.value.split(',').filter(id => !deletedAddons.has(id));
            orderInput.value = currentOrder.join(',');
            
            // Show empty state if no addons left
            const addonList = document.getElementById('addon-list');
            const remainingAddons = addonList.querySelectorAll('.addon-item[data-addon-id]');
            if (remainingAddons.length === 0) {
                const emptyState = document.getElementById('addon-empty-state');
                if (emptyState) {
                    emptyState.classList.remove('hidden');
                }
            }
        }
    }
    
    // Add event listener to the addon list using event delegation
    const addonList = document.getElementById('addon-list');
    if (addonList) {
        addonList.addEventListener('click', handleDeleteAddon);
    }
});

export {}
