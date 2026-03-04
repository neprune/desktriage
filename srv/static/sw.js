// Minimal service worker — required for PWA installability.
// No offline caching; just passes through all requests.
self.addEventListener('fetch', function(event) {
  event.respondWith(fetch(event.request));
});
