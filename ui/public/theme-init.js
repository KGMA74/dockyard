// Applies the saved theme before first paint, so the page never flashes the
// wrong mode. Kept as a tiny external file (not an inline <script>) so the
// Content-Security-Policy can stay at script-src 'self'.
;(function () {
  try {
    var saved = localStorage.getItem('dockyard_theme')
    var dark =
      saved === 'dark' ||
      (saved !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches)
    if (dark) document.documentElement.classList.add('dark')
  } catch (_) {
    // Storage blocked (private mode, strict settings): fall back to light.
  }
})()
