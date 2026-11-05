// Opens the app by its link a moment after the page shows, as t.me does
// (docs/tasks/026-research/tme.md): iOS through a hidden frame, with the
// page itself as the fallback after 2 s; elsewhere at once after 100 ms.
// The link is the page's main button; without the app nothing happens.
(function () {
  var button = document.querySelector('[data-app]');
  if (!button) return;
  var url = button.getAttribute('href');
  if (!url) return;
  var ios = /iPhone|iPad|iPod/.test(navigator.userAgent) ||
    (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
  if (ios) {
    var hidden = false;
    window.addEventListener('pagehide', function () { hidden = true; });
    window.addEventListener('blur', function () { hidden = true; });
    var frame = document.createElement('iframe');
    frame.style.display = 'none';
    frame.src = url;
    document.body.appendChild(frame);
    setTimeout(function () { if (!hidden) window.location = url; }, 2000);
  } else {
    setTimeout(function () { window.location = url; }, 100);
  }
})();
