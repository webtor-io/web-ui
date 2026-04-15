import av from '../../lib/av';
import DragDrop from '../../lib/dragAndDrop';
import { init as initI18n, t, tf } from '../../lib/profile/i18n';

av(async function(){
    await initI18n();
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
        
        if (confirm(tf('profile.addons.deleteConfirm', addonUrl))) {
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

            if (window.toast) window.toast.success(t('profile.addons.deleted'));
            
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
