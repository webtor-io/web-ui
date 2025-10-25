import av from '../../lib/av';
import DragAndDrop from '../../lib/dragAndDrop';

av(async function(){
    // Initialize drag and drop functionality for stremio resolution settings
    new DragAndDrop({
        listSelector: '#resolution-list',
        itemSelector: '.resolution-item',
        orderInputSelector: '#resolution_order',
        dataAttribute: 'data-resolution'
    });
});

export {}