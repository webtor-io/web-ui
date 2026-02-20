import av from '../lib/av';

av(function() {
    const ribbon = this;

    const modal = ribbon.querySelector('#discover-modal');
    if (!modal) return;
    const openBtns = ribbon.querySelectorAll('.discover-open');
    const closeBtns = modal.querySelectorAll('.discover-close');

    // Open modal
    openBtns.forEach(function(btn) {
        btn.addEventListener('click', function() {
            modal.showModal();
            if (window.umami) {
                window.umami.track('discover-modal-shown');
            }
        });
    });

    // Close modal on button click
    closeBtns.forEach(function(btn) {
        btn.addEventListener('click', function() {
            modal.close();
        });
    });

    // Track ribbon impression via IntersectionObserver
    if ('IntersectionObserver' in window) {
        var observer = new IntersectionObserver(function(entries) {
            entries.forEach(function(entry) {
                if (entry.isIntersecting) {
                    if (window.umami) {
                        window.umami.track('discover-shown');
                    }
                    observer.disconnect();
                }
            });
        }, { threshold: 0.3 });
        observer.observe(ribbon);
    }
});

export {}
