// Synchronous pre-paint boot cache reader for theme and UI zoom.
// Canonical settings live in config.toml; localStorage ('tisane:boot') prevents a 1-frame startup flash.
(function () {
  var theme = 'dark';
  var zoom = 100;
  try {
    var raw = window.localStorage.getItem('tisane:boot');
    if (raw) {
      var parsed = JSON.parse(raw);
      if (parsed && typeof parsed === 'object') {
        if (parsed.theme === 'light' || parsed.theme === 'dark' || parsed.theme === 'system') {
          theme = parsed.theme;
        }
        if (typeof parsed.uiZoom === 'number' && parsed.uiZoom >= 75 && parsed.uiZoom <= 200) {
          zoom = Math.round(parsed.uiZoom);
        }
      }
    }
  } catch {
    // Ignore storage access errors in restricted browsing contexts.
  }

  var resolved = theme;
  if (resolved === 'system') {
    var prefersLight =
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-color-scheme: light)').matches;
    resolved = prefersLight ? 'light' : 'dark';
  }

  var root = document.documentElement;
  root.dataset.theme = resolved;
  root.style.colorScheme = resolved;
  if (zoom !== 100) {
    root.style.zoom = String(zoom / 100);
  }
})();
