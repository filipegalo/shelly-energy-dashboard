document.addEventListener("DOMContentLoaded", () => {
  document.querySelectorAll(".network-chooser").forEach((select) => {
    select.addEventListener("change", () => {
      select.form.action = `/networks/${encodeURIComponent(select.value)}/scan`;
    });
  });
  document.querySelectorAll(".confirm-form").forEach((form) => {
    form.addEventListener("submit", (event) => {
      if (!window.confirm(form.dataset.confirm || "Continue?")) event.preventDefault();
    });
  });
  const progress = document.getElementById("progress-bar");
  if (progress) progress.style.width = `${Number(progress.dataset.percent) || 0}%`;
});
