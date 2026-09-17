(() => {
  let theme = "dark";
  try {
    const stored = localStorage.getItem("aipermission-theme");
    if (stored === "light" || stored === "dark") theme = stored;
  } catch {
    // Storage can be disabled by browser policy; the default remains usable.
  }
  document.documentElement.dataset.theme = theme;
})();
