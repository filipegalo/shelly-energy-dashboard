(() => {
  "use strict";

  const chartCanvas = document.querySelector("[data-room-energy-chart]");
  const rangeButtons = Array.from(document.querySelectorAll("[data-room-range]"));
  const modeControl = document.querySelector("[data-room-chart-mode]");
  const comparisonControl = document.querySelector("[data-room-house-comparison]");
  const chartStatus = document.querySelector("[data-room-chart-status]");
  const query = new URLSearchParams(window.location.search);
  let selectedRange = query.get("range") || rangeButtons.find((button) => button.classList.contains("active"))?.dataset.roomRange || "24h";
  let selectedMode = query.get("mode") || modeControl?.value || "consumption";
  let selectedQuery = new URLSearchParams(query);
  let chart = null;
  let lastPayload = null;
  let requestController = null;

  const palette = [
    [47, 204, 173],
    [157, 169, 255],
    [255, 144, 112],
    [246, 188, 87],
    [112, 190, 255],
    [202, 133, 255],
    [121, 211, 132],
    [255, 123, 180],
    [107, 185, 181],
    [184, 194, 202]
  ];

  const modeMetadata = {
    consumption: { field: "energy_kwh", label: "Energy", unit: "kWh", decimals: 2 },
    allocated_cost: { field: "allocated_cost_eur", label: "Allocated variable energy cost", unit: "€", decimals: 2 },
    average_power: { field: "average_power_w", label: "Average power", unit: "W", decimals: 0 },
    peak_power: { field: "peak_power_w", label: "Peak power", unit: "W", decimals: 0 },
    house_share: { field: "house_share", label: "House share", unit: "%", decimals: 1 }
  };

  const numberOrNull = (value) => {
    if (value === null || value === undefined || value === "") return null;
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  };

  const timezoneOf = (payload) => payload?.range?.timezone || payload?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone;

  const formatDateLabel = (value, interval, timezone) => {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return String(value);
    const options = interval === "day"
      ? { month: "short", day: "numeric", timeZone: timezone }
      : { hour: "2-digit", minute: "2-digit", month: "short", day: "numeric", timeZone: timezone };
    try {
      return new Intl.DateTimeFormat(undefined, options).format(date);
    } catch (_) {
      delete options.timeZone;
      return new Intl.DateTimeFormat(undefined, options).format(date);
    }
  };

  const rgba = (color, alpha) => `rgba(${color[0]}, ${color[1]}, ${color[2]}, ${alpha})`;

  const pointColor = (color, point, solid = false) => {
    const quality = String(point?.quality || "complete");
    if (quality === "reconciliation_warning" || quality === "warning") return solid ? "#ff7777" : "rgba(255, 107, 107, .62)";
    if (quality === "partial" || quality === "stale" || quality === "estimated") return solid ? "#efb95c" : "rgba(246, 188, 87, .56)";
    if (quality === "missing") return solid ? "#8a4247" : "rgba(138, 66, 71, .20)";
    return rgba(color, solid ? .98 : .70);
  };

  const allStarts = (payload, kind) => {
    const values = [];
    if (kind === "reconciliation") {
      for (const point of payload.reconciliation_points || payload.points || []) if (point?.start) values.push(point.start);
    } else {
      for (const series of payload.series || []) for (const point of series.points || []) if (point?.start) values.push(point.start);
      for (const point of payload.whole_house_points || []) if (point?.start) values.push(point.start);
    }
    return Array.from(new Set(values)).sort((left, right) => new Date(left).getTime() - new Date(right).getTime());
  };

  const alignedPoints = (points, starts) => {
    const byStart = new Map((points || []).map((point) => [point.start, point]));
    return starts.map((start) => byStart.get(start) || { start, quality: "missing" });
  };

  const axisTick = (metadata) => (value) => metadata.unit === "€"
    ? `€${Number(value).toFixed(metadata.decimals)}`
    : `${Number(value).toFixed(metadata.decimals)}${metadata.unit === "%" ? "%" : ""}`;

  const chartOptions = (metadata, stacked) => ({
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: "index", intersect: false },
    plugins: {
      legend: { labels: { color: "#b9c4cc", usePointStyle: true, boxWidth: 9 } },
      tooltip: {
        callbacks: {
          label: (context) => {
            if (context.raw === null) return `${context.dataset.label}: missing`;
            const value = Number(context.raw);
            if (context.dataset.yAxisID === "cost") return `${context.dataset.label}: €${value.toFixed(2)}`;
            if (metadata.unit === "€") return `${context.dataset.label}: €${value.toFixed(2)}`;
            return `${context.dataset.label}: ${value.toFixed(metadata.decimals)} ${metadata.unit}`.trim();
          }
        }
      }
    },
    scales: {
      x: { stacked, ticks: { color: "#7f8b94", maxRotation: 0, autoSkip: true }, grid: { display: false } },
      primary: { stacked, beginAtZero: true, position: "left", ticks: { color: "#7f8b94", callback: axisTick(metadata) }, grid: { color: "rgba(255,255,255,.06)" }, title: { display: true, text: `${metadata.label} (${metadata.unit})`, color: "#91a0ad" } },
      cost: { beginAtZero: true, position: "right", display: selectedMode === "consumption" && chartCanvas?.dataset.chartKind === "room", ticks: { color: "#9da9ff", callback: (value) => `€${value}` }, grid: { display: false }, title: { display: true, text: "Allocated cost (€)", color: "#9da9ff" } }
    }
  });

  const seriesDatasets = (payload, starts, kind) => {
    const metadata = modeMetadata[selectedMode] || modeMetadata.consumption;
    const datasets = [];
    (payload.series || []).forEach((series, index) => {
      const color = series.virtual ? [160, 169, 176] : palette[index % palette.length];
      const points = alignedPoints(series.points, starts);
      datasets.push({
        label: series.name || `Room ${series.room_id}`,
        data: points.map((point) => numberOrNull(point[metadata.field])),
        backgroundColor: points.map((point) => pointColor(color, point)),
        borderColor: points.map((point) => pointColor(color, point, true)),
        borderWidth: selectedMode === "house_share" ? 2 : 1,
        borderRadius: 3,
        spanGaps: false,
        type: selectedMode === "house_share" ? "line" : "bar",
        pointRadius: selectedMode === "house_share" ? 2 : 0,
        tension: .22,
        yAxisID: "primary"
      });
    });

    if (kind === "room" && selectedMode === "consumption") {
      const roomSeries = payload.series?.[0];
      if (roomSeries) {
        const points = alignedPoints(roomSeries.points, starts);
        datasets.push({
          label: "Allocated variable energy cost",
          data: points.map((point) => numberOrNull(point.allocated_cost_eur)),
          type: "line",
          borderColor: "#9da9ff",
          backgroundColor: "#9da9ff",
          pointRadius: 2,
          tension: .25,
          spanGaps: false,
          yAxisID: "cost"
        });
      }
    }

    const compareWholeHouse = kind === "overview" || (kind === "room" && comparisonControl?.checked);
    if (selectedMode === "consumption" && compareWholeHouse && payload.whole_house_points) {
      const points = alignedPoints(payload.whole_house_points, starts);
      datasets.push({
        label: "Whole house",
        data: points.map((point) => numberOrNull(point.energy_kwh)),
        type: "line",
        borderColor: "#f5f7f8",
        backgroundColor: "#f5f7f8",
        borderDash: [6, 4],
        pointRadius: 0,
        tension: .18,
        spanGaps: false,
        yAxisID: "primary"
      });
    }
    return datasets;
  };

  const reconciliationDatasets = (payload, starts) => {
    const points = alignedPoints(payload.reconciliation_points || payload.points, starts);
    return [
      { label: "Whole house", field: "whole_house_energy_kwh", color: [245, 247, 248], type: "line" },
      { label: "Room meters", field: "room_energy_kwh", color: [47, 204, 173], type: "line" },
      { label: "Unmetered / Other", field: "unmetered_energy_kwh", color: [157, 169, 255], type: "bar" }
    ].map((definition) => ({
      label: definition.label,
      data: points.map((point) => numberOrNull(point[definition.field])),
      type: definition.type,
      backgroundColor: points.map((point) => pointColor(definition.color, point, definition.type === "line")),
      borderColor: points.map((point) => pointColor(definition.color, point, true)),
      borderWidth: definition.type === "line" ? 2 : 1,
      borderRadius: 3,
      pointRadius: definition.type === "line" ? 2 : 0,
      tension: .2,
      spanGaps: false,
      yAxisID: "primary"
    }));
  };

  const qualitySummary = (payload, kind) => {
    const points = kind === "reconciliation"
      ? (payload.reconciliation_points || payload.points || [])
      : (payload.series || []).flatMap((series) => series.points || []);
    const counts = points.reduce((result, point) => {
      const quality = String(point?.quality || "complete");
      if (quality === "missing") result.missing += 1;
      else if (["partial", "stale", "estimated"].includes(quality)) result.partial += 1;
      else if (["warning", "reconciliation_warning"].includes(quality)) result.warning += 1;
      return result;
    }, { missing: 0, partial: 0, warning: 0 });
    const messages = [];
    if (counts.warning) messages.push(`${counts.warning} reconciliation warning${counts.warning === 1 ? "" : "s"}`);
    if (counts.partial) messages.push(`${counts.partial} partial or stale value${counts.partial === 1 ? "" : "s"}`);
    if (counts.missing) messages.push(`${counts.missing} missing value${counts.missing === 1 ? "" : "s"} shown as gaps`);
    return messages.join(" · ");
  };

  const renderChart = (payload) => {
    if (!chartCanvas || !window.Chart) return;
    const kind = chartCanvas.dataset.chartKind || "overview";
    const starts = allStarts(payload, kind);
    const metadata = kind === "reconciliation" ? modeMetadata.consumption : (modeMetadata[selectedMode] || modeMetadata.consumption);
    const datasets = kind === "reconciliation"
      ? reconciliationDatasets(payload, starts)
      : seriesDatasets(payload, starts, kind);
    const stacked = kind === "overview" && !["house_share"].includes(selectedMode);
    const labels = starts.map((start) => formatDateLabel(start, payload.interval, timezoneOf(payload)));
    if (chart) chart.destroy();
    chart = new window.Chart(chartCanvas, {
      type: "bar",
      data: { labels, datasets },
      options: chartOptions(metadata, stacked)
    });
    const hasData = datasets.some((dataset) => dataset.data.some((value) => value !== null));
    const quality = qualitySummary(payload, kind);
    if (chartStatus) {
      chartStatus.textContent = hasData ? quality : "No readings are available in this range.";
      chartStatus.hidden = hasData && !quality;
    }
    chartCanvas.setAttribute("aria-busy", "false");
  };

  const loadChart = async (parameters, updateLocation = false) => {
    if (!chartCanvas) return;
    requestController?.abort();
    requestController = new AbortController();
    selectedQuery = new URLSearchParams(parameters);
    selectedQuery.set("range", selectedRange);
    if (chartCanvas.dataset.chartKind !== "reconciliation") selectedQuery.set("mode", selectedMode);
    rangeButtons.forEach((button) => button.classList.toggle("active", button.dataset.roomRange === selectedRange));
    chartCanvas.setAttribute("aria-busy", "true");
    if (chartStatus) {
      chartStatus.hidden = false;
      chartStatus.textContent = "Loading history…";
    }
    try {
      const response = await fetch(`${chartCanvas.dataset.endpoint}?${selectedQuery}`, {
        headers: { Accept: "application/json" },
        cache: "no-store",
        signal: requestController.signal
      });
      if (!response.ok) throw new Error(`History request failed (${response.status})`);
      lastPayload = await response.json();
      renderChart(lastPayload);
      if (updateLocation) window.history.replaceState(null, "", `${window.location.pathname}?${selectedQuery}`);
    } catch (error) {
      if (error?.name === "AbortError") return;
      chartCanvas.setAttribute("aria-busy", "false");
      if (chartStatus) {
        chartStatus.hidden = false;
        chartStatus.textContent = error instanceof Error ? error.message : "Could not load room energy history.";
      }
    }
  };

  rangeButtons.forEach((button) => button.addEventListener("click", () => {
    selectedRange = button.dataset.roomRange;
    const next = new URLSearchParams();
    loadChart(next, true);
  }));

  modeControl?.addEventListener("change", () => {
    selectedMode = modeControl.value;
    if (lastPayload) renderChart(lastPayload);
    selectedQuery.set("mode", selectedMode);
    window.history.replaceState(null, "", `${window.location.pathname}?${selectedQuery}`);
  });

  comparisonControl?.addEventListener("change", () => {
    if (lastPayload) renderChart(lastPayload);
  });

  document.querySelector("[data-room-custom-range]")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const values = new FormData(event.currentTarget);
    selectedRange = "custom";
    loadChart(new URLSearchParams({
      start: String(values.get("start") || ""),
      end: String(values.get("end") || "")
    }), true);
  });

  if (chartCanvas) {
    if (modeControl && modeMetadata[selectedMode]) modeControl.value = selectedMode;
    loadChart(selectedQuery);
  }

  const parseDisplayedNumber = (value) => {
    const raw = String(value || "").trim();
    if (!raw || raw.includes("—")) return null;
    let numeric = raw.replace(/[^0-9,.-]/g, "");
    if (!numeric) return null;
    const comma = numeric.lastIndexOf(",");
    const dot = numeric.lastIndexOf(".");
    if (comma > dot) numeric = numeric.replace(/\./g, "").replace(",", ".");
    else numeric = numeric.replace(/,/g, "");
    const parsed = Number(numeric);
    return Number.isFinite(parsed) ? parsed : null;
  };

  const rankingSelect = document.querySelector("[data-room-ranking]");
  const rankingList = document.querySelector("[data-room-ranking-list]");
  const refreshRanking = () => {
    if (!rankingSelect || !rankingList) return;
    const key = rankingSelect.value;
    const items = Array.from(rankingList.querySelectorAll("[data-ranking-item]"));
    items.forEach((item) => {
      const missing = item.dataset.quality === "missing" || parseDisplayedNumber(item.dataset[key]) === null;
      item.classList.toggle("is-missing", missing);
      const value = item.querySelector("[data-ranking-value]");
      if (value) value.textContent = missing ? "Not ranked — missing data" : item.dataset[key];
    });
    items.sort((left, right) => {
      const leftValue = left.classList.contains("is-missing") ? Number.NEGATIVE_INFINITY : parseDisplayedNumber(left.dataset[key]);
      const rightValue = right.classList.contains("is-missing") ? Number.NEGATIVE_INFINITY : parseDisplayedNumber(right.dataset[key]);
      return (rightValue ?? Number.NEGATIVE_INFINITY) - (leftValue ?? Number.NEGATIVE_INFINITY);
    });
    items.forEach((item) => rankingList.appendChild(item));
  };
  rankingSelect?.addEventListener("change", refreshRanking);
  refreshRanking();

  const syncEffectivePreset = (select) => {
    const form = select.closest("form");
    const custom = form?.querySelector("[data-room-custom-effective]");
    const input = custom?.querySelector("input[name=effective_at]");
    const visible = select.value === "custom";
    if (custom) custom.hidden = !visible;
    if (input) input.required = visible;
  };

  const syncParentOptions = (form) => {
    const room = form.querySelector("[data-room-select]")?.value;
    const parent = form.querySelector("select[name=parent_assignment_id]");
    if (!parent) return;
    Array.from(parent.options).forEach((option) => {
      if (!option.dataset.roomId) return;
      const allowed = Boolean(room) && option.dataset.roomId === room;
      option.hidden = !allowed;
      option.disabled = !allowed;
      if (!allowed && option.selected) parent.value = "0";
    });
  };

  const syncRole = (select) => {
    const form = select.closest("form");
    const field = form?.querySelector("[data-room-parent-field]");
    const parent = field?.querySelector("select[name=parent_assignment_id]");
    const needsParent = select.value === "submeter";
    if (field) field.hidden = !needsParent;
    if (parent) parent.required = needsParent;
    if (!needsParent && parent) parent.value = "0";
    if (form) syncParentOptions(form);
  };

  const initializeDynamicForms = (root = document) => {
    root.querySelectorAll("[data-room-effective-preset]").forEach(syncEffectivePreset);
    root.querySelectorAll("[data-room-role]").forEach(syncRole);
    root.querySelectorAll("form").forEach(syncParentOptions);
  };

  document.addEventListener("change", (event) => {
    if (event.target.matches("[data-room-effective-preset]")) syncEffectivePreset(event.target);
    if (event.target.matches("[data-room-role]")) syncRole(event.target);
    if (event.target.matches("[data-room-select]")) syncParentOptions(event.target.closest("form"));
  });
  document.body.addEventListener("htmx:afterSwap", (event) => initializeDynamicForms(event.detail.target));
  initializeDynamicForms();

  const liveTargets = document.querySelectorAll("[data-room-card], [data-room-current-power][data-room-id]");
  const formatPower = (watts) => watts >= 1000 ? `${(watts / 1000).toFixed(2)} kW` : `${Math.round(watts)} W`;
  const liveQualityClasses = [
    "quality-complete", "quality-Complete", "quality-partial", "quality-Partial",
    "quality-missing", "quality-Missing", "quality-stale", "quality-Stale",
    "quality-warning", "quality-Warning", "quality-reconciliation_warning"
  ];
  const applyLiveQuality = (node, quality) => {
    if (!node) return;
    liveQualityClasses.forEach((name) => node.classList.remove(name));
    node.classList.add(`quality-${quality}`);
  };
  const relativeLiveTime = (timestamp) => {
    if (!timestamp) return "No live reading";
    const ageSeconds = Math.max(0, Math.round((Date.now() - new Date(timestamp).getTime()) / 1000));
    if (!Number.isFinite(ageSeconds)) return "No live reading";
    if (ageSeconds < 60) return `Updated ${ageSeconds}s ago`;
    return `Updated ${Math.floor(ageSeconds / 60)}m ago`;
  };
  const resetLiveTargets = () => {
    document.querySelectorAll("[data-room-current-power][data-room-id]").forEach((node) => { node.textContent = "—"; });
    document.querySelectorAll("[data-room-card][data-room-id]").forEach((card) => {
      const power = card.querySelector("[data-room-current-power]");
      const sources = card.querySelector("[data-room-sources]");
      const status = card.querySelector("[data-room-quality]");
      const updated = card.querySelector("[data-room-last-update]");
      if (power) power.textContent = "—";
      if (sources) sources.textContent = "0 of 0 sources reporting";
      if (status) {
        applyLiveQuality(status, "missing");
        status.textContent = "missing";
      }
      if (updated) updated.textContent = "No live reading";
    });
    document.querySelectorAll("[data-assignment-id]").forEach((source) => {
      const power = source.querySelector("[data-source-current-power]");
      const status = source.querySelector("[data-source-status]");
      const freshness = source.querySelector("[data-source-freshness]");
      if (power) power.textContent = "—";
      if (status) {
        applyLiveQuality(status, "missing");
        status.textContent = "Unavailable";
      }
      if (freshness) freshness.textContent = "No live reading";
    });
  };
  const updateSourceLiveTarget = (assignment, live, power) => {
    const assignmentID = String(assignment.assignment_id || "");
    if (!assignmentID) return;
    document.querySelectorAll("[data-assignment-id]").forEach((source) => {
      if (source.dataset.assignmentId !== assignmentID) return;
      const powerNode = source.querySelector("[data-source-current-power]");
      const statusNode = source.querySelector("[data-source-status]");
      const freshnessNode = source.querySelector("[data-source-freshness]");
      const quality = !live.online ? "missing" : (live.stale ? "stale" : (power === null ? "partial" : "complete"));
      const state = !live.online ? (live.last_error || "Offline") : (live.stale ? "Stale" : "Online");
      if (powerNode) powerNode.textContent = power === null ? "—" : formatPower(power);
      if (statusNode) {
        applyLiveQuality(statusNode, quality);
        statusNode.textContent = state;
      }
      if (freshnessNode) freshnessNode.textContent = relativeLiveTime(live.reading?.timestamp);
    });
  };
  const updateLiveSnapshot = (snapshot) => {
    if (!snapshot?.sources) return;
    resetLiveTargets();
    const rooms = new Map();
    snapshot.sources.forEach((source) => {
      const live = source.live || {};
      const power = numberOrNull(live.reading?.power_w);
      const fresh = live.online === true && live.stale !== true && power !== null;
      (source.assignments || []).forEach((assignment) => {
        updateSourceLiveTarget(assignment, live, power);
        if (assignment.aggregation_role !== "primary") return;
        const key = String(assignment.room_id);
        const value = rooms.get(key) || { power: 0, expected: 0, reporting: 0, stale: 0 };
        value.expected += 1;
        if (fresh) {
          value.power += power;
          value.reporting += 1;
        } else if (live.stale === true) {
          value.stale += 1;
        }
        rooms.set(key, value);
      });
    });
    rooms.forEach((value, roomID) => {
      const quality = value.reporting === value.expected
        ? "complete"
        : (value.reporting > 0 ? "partial" : (value.stale > 0 ? "stale" : "missing"));
      document.querySelectorAll("[data-room-id]").forEach((container) => {
        if (container.dataset.roomId !== roomID) return;
        const power = container.matches("[data-room-current-power]") ? container : container.querySelector("[data-room-current-power]");
        const sources = container.querySelector?.("[data-room-sources]");
        const status = container.querySelector?.("[data-room-quality]");
        const updated = container.querySelector?.("[data-room-last-update]");
        if (power) power.textContent = value.reporting ? formatPower(value.power) : "—";
        if (sources) sources.textContent = `${value.reporting} of ${value.expected} sources reporting`;
        if (status) {
          applyLiveQuality(status, quality);
          status.textContent = quality;
        }
        if (updated) updated.textContent = "Updated just now";
      });
    });
  };

  if (liveTargets.length && window.EventSource) {
    const stream = new EventSource("/energy/rooms/events");
    const receive = (event) => {
      try {
        const payload = JSON.parse(event.data);
        updateLiveSnapshot(payload.snapshot || payload);
      } catch (_) {
        // Keep the last server-rendered values if a malformed event is received.
      }
    };
    ["room-energy-snapshot", "room-energy-reading", "room-source-online", "room-source-offline", "room-source-stale", "assignment-changed"].forEach((name) => stream.addEventListener(name, receive));
  }
})();
