// Applies the saved light/dark choice to <html data-theme> before first paint
// (issue #978). A classic, blocking script in <head> (a module would run after
// the first paint and flash the wrong theme); CSP allows it as 'self'. The key
// and values mirror static/src/components/lib/theme.ts.
(function () {
  try {
    var saved = localStorage.getItem('pitha-theme');
    if (saved === 'light' || saved === 'dark') {
      document.documentElement.setAttribute('data-theme', saved);
    }
  } catch (e) {
    // Storage blocked: follow the OS.
  }
})();
