// Polls the login request of the page every 2 s (task 106): the user
// answers in the app; once answered, the browser goes back to the site. The
// state is bound to this browser by a cookie the page set.
(function () {
  var pending = document.getElementById('oauth-pending');
  if (!pending) return;
  var token = pending.getAttribute('data-token');
  if (!token) return;
  var url = '/auth/state?token=' + encodeURIComponent(token);
  var delay = 2000;
  var stopped = false;

  function show(id) {
    pending.hidden = true;
    var el = document.getElementById(id);
    if (el) el.hidden = false;
  }

  function next(ms) {
    if (!stopped) setTimeout(poll, ms);
  }

  function poll() {
    var xhr = new XMLHttpRequest();
    xhr.open('GET', url, true);
    xhr.setRequestHeader('Accept', 'application/json');
    xhr.onload = function () {
      var state = null;
      try { state = JSON.parse(xhr.responseText); } catch (e) { state = null; }
      if (xhr.status !== 200 || !state) {
        // A failure of the server or the network: try again, slower.
        next(Math.min(delay * 2, 10000));
        return;
      }
      if (state.status === 'pending') {
        next(delay);
      } else if (state.status === 'done' && state.redirect) {
        stopped = true;
        show('oauth-done');
        window.location.replace(state.redirect);
      } else if (state.status === 'declined') {
        stopped = true;
        show('oauth-declined');
      } else {
        stopped = true;
        show('oauth-expired');
      }
    };
    xhr.onerror = function () { next(Math.min(delay * 2, 10000)); };
    xhr.send();
  }

  next(delay);
})();
