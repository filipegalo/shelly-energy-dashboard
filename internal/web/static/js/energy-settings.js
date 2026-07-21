(() => {
  const form = document.getElementById("energy-pricing-form");
  if (!form) return;
  let timer = null;
  const refresh = async () => {
    const values = new URLSearchParams();
    for (const [name, value] of new FormData(form)) {
      if (name !== "_csrf" && name !== "effective_from") values.append(name, value);
    }
    try {
      const response = await fetch(`/energy/pricing-preview?${values}`, { headers: { Accept: "application/json" } });
      if (!response.ok) return;
      const payload = await response.json();
      payload.previews.forEach((preview) => {
        const target = document.querySelector(`[data-preview-consumption="${preview.consumption_kwh}"] b`);
        if (target) target.textContent = preview.total_eur;
      });
      const allowance = document.getElementById("energy-vat-preview");
      if (allowance) allowance.textContent = payload.allowance_kwh;
    } catch (_) {
      // Keep the last valid server-rendered preview while fields are incomplete.
    }
  };
  const schedule = () => {
    window.clearTimeout(timer);
    timer = window.setTimeout(refresh, 250);
  };
  form.addEventListener("input", schedule);
  form.addEventListener("change", schedule);
})();
