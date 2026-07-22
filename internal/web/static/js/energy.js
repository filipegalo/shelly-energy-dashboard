(() => {
  const canvas = document.getElementById("energy-chart");
  if (!canvas) return;

  const status = document.getElementById("energy-chart-status");
  const rangeButtons = Array.from(document.querySelectorAll("[data-energy-range]"));
  const pageURL = canvas.dataset.energyPage || window.location.pathname;
  const refreshDashboardPartials = canvas.dataset.refreshPartials === "true";
  let chart = null;
  let selectedRange = rangeButtons.find((button) => button.classList.contains("active"))?.dataset.energyRange || "24h";
  let selectedQuery = `range=${encodeURIComponent(selectedRange)}`;

  const numberOrNull = (value) => {
    const parsed = Number.parseFloat(value);
    return Number.isFinite(parsed) ? parsed : null;
  };
  const formatLabel = (value, interval, timezone) => {
    const date = new Date(value);
    return interval === "day"
      ? new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", timeZone: timezone }).format(date)
      : new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", month: "short", day: "numeric", timeZone: timezone, timeZoneName: "short" }).format(date);
  };
  const chartConfig = (payload) => {
    const labels = payload.points.map((point) => formatLabel(point.start, payload.interval, payload.range.timezone));
    const partialColors = payload.points.map((point) => point.quality === "partial" || point.sample_count === 0 ? "rgba(243, 174, 63, .55)" : "rgba(47, 204, 173, .72)");
    return {
      type: "bar",
      data: {
        labels,
        datasets: [
          { label: "Energy (kWh)", data: payload.points.map((point) => numberOrNull(point.energy_kwh)), backgroundColor: partialColors, borderRadius: 4, yAxisID: "energy" },
          { label: "Estimated cost (€)", data: payload.points.map((point) => numberOrNull(point.cost_eur)), type: "line", borderColor: "#9da9ff", backgroundColor: "#9da9ff", pointRadius: 2, tension: .25, yAxisID: "cost" }
        ]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        interaction: { mode: "index", intersect: false },
        plugins: { legend: { labels: { color: "#b9c4cc" } } },
        scales: {
          x: { ticks: { color: "#7f8b94", maxRotation: 0, autoSkip: true }, grid: { display: false } },
          energy: { beginAtZero: true, position: "left", ticks: { color: "#7f8b94" }, grid: { color: "rgba(255,255,255,.06)" } },
          cost: { beginAtZero: true, position: "right", ticks: { color: "#9da9ff", callback: (value) => `€${value}` }, grid: { display: false } }
        }
      }
    };
  };
  const refreshPartials = (query) => {
    if (!window.htmx) return;
    window.htmx.ajax("GET", `/energy/partials/summary?${query}`, { target: "#energy-summary", swap: "outerHTML" });
    window.htmx.ajax("GET", `/energy/partials/cost-breakdown?${query}`, { target: "#energy-cost-breakdown", swap: "outerHTML" });
  };
  const loadHistory = async (range, query = `range=${encodeURIComponent(range)}`, refreshSummary = false) => {
    selectedRange = range;
    selectedQuery = query;
    rangeButtons.forEach((button) => button.classList.toggle("active", button.dataset.energyRange === range));
    status.hidden = false;
    status.textContent = "Loading history…";
    try {
      const response = await fetch(`/energy/history?${query}`, { headers: { Accept: "application/json" } });
      if (!response.ok) throw new Error(`History request failed (${response.status})`);
      const payload = await response.json();
      if (chart) chart.destroy();
      chart = new window.Chart(canvas, chartConfig(payload));
      const hasData = payload.points.some((point) => point.energy_kwh !== null);
      status.textContent = hasData ? "" : "No readings in this range yet.";
      status.hidden = hasData;
      window.history.replaceState(null, "", `${pageURL}?${query}`);
      if (refreshSummary && refreshDashboardPartials) refreshPartials(query);
    } catch (error) {
      status.hidden = false;
      status.textContent = error instanceof Error ? error.message : "Could not load energy history.";
    }
  };

  rangeButtons.forEach((button) => button.addEventListener("click", () => loadHistory(button.dataset.energyRange, undefined, true)));
  const customRange = document.getElementById("energy-custom-range");
  customRange?.addEventListener("submit", (event) => {
    event.preventDefault();
    const values = new FormData(customRange);
    const query = new URLSearchParams({ range: "custom", start: String(values.get("start") || ""), end: String(values.get("end") || "") }).toString();
    loadHistory("custom", query, true);
  });
  loadHistory(selectedRange, new URLSearchParams(window.location.search).toString() || selectedQuery);

  const formatPower = (watts) => watts >= 1000 ? `${(watts / 1000).toFixed(2)} kW` : `${Math.round(watts)} W`;
  const formatValue = (value, unit) => Number.isFinite(value) ? `${value.toFixed(2)} ${unit}` : "—";
  const updateText = (id, value) => {
    const node = document.getElementById(id);
    if (node) node.textContent = value;
  };
  const livePanel = document.querySelector("[data-energy-live-panel]");
  const liveState = document.querySelector("[data-energy-live-state]");
  const freshnessValue = document.querySelector("[data-energy-freshness-value]");
  const freshnessDetail = document.querySelector("[data-energy-freshness-detail]");
  const healthCard = document.querySelector("[data-energy-meter-health]");
  const healthIcon = document.querySelector("[data-energy-health-icon]");
  const healthTitle = document.querySelector("[data-energy-health-title]");
  const healthMessage = document.querySelector("[data-energy-health-message]");
  const healthLink = document.querySelector("[data-energy-health-link]");
  const setStateClass = (node, stateClass, classes) => {
    if (!node) return;
    classes.forEach((name) => node.classList.remove(name));
    node.classList.add(stateClass);
  };
  let lastChartRefresh = 0;
  const stream = new EventSource("/energy/events");
  const updateLive = (event) => {
    const state = JSON.parse(event.data);
    const reading = state.reading || {};
    updateText("energy-live-power", Number.isFinite(reading.power_w) ? formatPower(reading.power_w) : "—");
    updateText("energy-live-voltage", formatValue(reading.voltage_v, "V"));
    updateText("energy-live-current", formatValue(reading.current_a, "A"));
    updateText("energy-live-pf", formatValue(reading.power_factor, ""));
    const readingAge = reading.timestamp ? Math.max(0, Math.round((Date.now() - new Date(reading.timestamp).getTime()) / 1000)) : null;
    const lastKnown = `Last known reading${readingAge === null ? "" : ` · ${readingAge}s ago`}`;
    const liveStatus = !state.online
      ? `${state.last_error || "Meter offline"} · ${lastKnown}`
      : (state.stale ? lastKnown : "Updated just now");
    updateText("energy-live-status", liveStatus);
    const stateKind = !state.online ? "error" : (state.stale ? "warning" : "healthy");
    livePanel?.classList.toggle("energy-stale", state.stale === true);
    setStateClass(liveState, `is-${stateKind}`, ["is-healthy", "is-warning", "is-error"]);
    if (freshnessValue) {
      freshnessValue.textContent = stateKind === "healthy" ? "Live" : (stateKind === "warning" ? "Stale" : "Offline");
      setStateClass(freshnessValue, stateKind === "healthy" ? "green" : (stateKind === "warning" ? "amber" : "red"), ["green", "amber", "red"]);
    }
    if (freshnessDetail) freshnessDetail.textContent = liveStatus;
    if (healthCard) {
      setStateClass(healthCard, `is-${stateKind}`, ["is-healthy", "is-warning", "is-error"]);
      if (healthIcon) healthIcon.textContent = stateKind === "healthy" ? "✓" : "!";
      if (healthTitle) healthTitle.textContent = stateKind === "healthy" ? "Main meter is reporting" : (stateKind === "warning" ? "Main meter reading is stale" : "Main meter offline");
      if (healthMessage) healthMessage.textContent = liveStatus;
      if (healthLink) {
        healthLink.href = stateKind === "error" ? "/energy/settings" : "/energy";
        healthLink.textContent = stateKind === "error" ? "Review meter" : "View details";
      }
    }
    const now = Date.now();
    if (now - lastChartRefresh > 60000) {
      lastChartRefresh = now;
      loadHistory(selectedRange, selectedQuery);
    }
  };
  ["energy-reading", "meter-offline", "meter-stale"].forEach((name) => stream.addEventListener(name, updateLive));
})();
