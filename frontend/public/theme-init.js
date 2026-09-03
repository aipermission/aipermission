(() => {
  const theme = localStorage.getItem("aipermission-theme") || "dark";
  document.documentElement.dataset.theme = theme;
})();
