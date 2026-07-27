const presets = {
  "slow-creep": { rps: 80, slow: 18, work: 1600, timeout: 120, pool: 24, queue: 80 },
  "traffic-spike": { rps: 190, slow: 12, work: 1100, timeout: 100, pool: 20, queue: 60 },
  "stalled-upstream": { rps: 65, slow: 48, work: 4200, timeout: 180, pool: 16, queue: 40 },
};

const diagnostics = {
  detached: "The timeout rate is flat, but every timed-out request leaves a worker behind. Group stack hashes to reveal one repeated blocked-send site.",
  context_aware: "Request and worker lifecycles end together. Cancellation events prove the timeout propagated beyond the handler.",
  buffered: "The goroutine count recovers because late sends cannot block. Post-timeout work remains high: buffering is cleanup, not cancellation.",
  bounded_pool: "Runtime growth stops at the pool limit. Queue delay and rejection now carry the overload signal that leaked goroutines previously hid.",
};

const queries = {
  detached: 'from spans | where goroutine.state == "chan send" | group by goroutine.stack_hash | count(), max(duration)',
  context_aware: 'from spans | where work.context_canceled == true | group by service.name | count(), p99(request.duration)',
  buffered: 'from spans | where worker.result_delivery == "buffer accepts late result" | sum(worker.post_timeout_ms)',
  bounded_pool: 'from spans | group by worker.pool_slot | max(pool.inflight), p99(pool.queue_ms), count_if(outcome == "rejected")',
};

const controls = {
  rps: document.querySelector("#rps"),
  slow: document.querySelector("#slow"),
  work: document.querySelector("#work"),
  timeout: document.querySelector("#timeout"),
  pool: document.querySelector("#pool"),
  queue: document.querySelector("#queue"),
};

let responseData = null;
let selectedPolicy = "detached";

const number = new Intl.NumberFormat("en-US");
const compact = new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 });

function syncControls() {
  const labels = {
    rps: `${controls.rps.value}/s`,
    slow: `${controls.slow.value}%`,
    work: `${number.format(controls.work.value)}ms`,
    timeout: `${number.format(controls.timeout.value)}ms`,
    pool: controls.pool.value,
    queue: controls.queue.value,
  };
  Object.entries(controls).forEach(([key, input]) => {
    document.querySelector(`#${key}-output`).textContent = labels[key];
    const progress = ((input.value - input.min) / (input.max - input.min)) * 100;
    input.style.setProperty("--fill", `${progress}%`);
  });
}

function configFromControls() {
  return {
    requestsPerSecond: Number(controls.rps.value),
    timeoutMs: Number(controls.timeout.value),
    slowWorkPercent: Number(controls.slow.value),
    slowWorkMs: Number(controls.work.value),
    fastWorkMs: 45,
    poolSize: Number(controls.pool.value),
    queueSize: Number(controls.queue.value),
    runSeconds: 90,
    seed: 6842,
  };
}

async function runSimulation(config = null) {
  const button = document.querySelector("#run-button");
  const status = document.querySelector("#run-status");
  button.disabled = true;
  status.classList.add("running");
  status.lastChild.textContent = " SIMULATING";

  try {
    const options = config ? {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(config),
    } : {};
    const response = await fetch("/api/simulate", options);
    if (!response.ok) throw new Error(`simulation returned ${response.status}`);
    responseData = await response.json();
    render();
    status.lastChild.textContent = " RUN COMPLETE";
  } catch (error) {
    status.lastChild.textContent = " RUN FAILED";
    document.querySelector("#strategy-grid").innerHTML = `<div class="loading-card">${escapeHTML(error.message)}</div>`;
  } finally {
    button.disabled = false;
    status.classList.remove("running");
  }
}

function render() {
  renderCards();
  renderTabs();
  renderEvidence();
}

function renderCards() {
  document.querySelector("#strategy-grid").innerHTML = responseData.strategies.map((strategy, index) => `
    <article class="strategy-card ${strategy.policy === selectedPolicy ? "selected" : ""}" data-policy="${strategy.policy}" style="--strategy-color:${strategy.color}" tabindex="0" role="button" aria-label="Inspect ${escapeHTML(strategy.name)}">
      <div class="card-top">
        <span class="card-number">0${index + 1} / STRATEGY</span>
        ${strategy.recommended ? '<span class="recommended">LIFECYCLE SAFE</span>' : ""}
      </div>
      <h3>${escapeHTML(strategy.name)}</h3>
      <span class="strategy-kicker">${escapeHTML(strategy.kicker)}</span>
      <p>${escapeHTML(strategy.description)}</p>
      <div class="card-metric">
        <strong>${compact.format(strategy.metrics.finalGoroutines)}</strong>
        <span>goroutines alive at t+90s</span>
      </div>
      <div class="card-bottom">
        <span>TIMEOUTS <b>${number.format(strategy.metrics.timedOut)}</b></span>
        <span>REJECTED <b>${number.format(strategy.metrics.rejected)}</b></span>
      </div>
    </article>
  `).join("");

  document.querySelectorAll(".strategy-card").forEach((card) => {
    const select = () => selectPolicy(card.dataset.policy);
    card.addEventListener("click", select);
    card.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        select();
      }
    });
  });
}

function renderTabs() {
  document.querySelector("#policy-tabs").innerHTML = responseData.strategies.map((strategy, index) => `
    <button class="policy-tab ${strategy.policy === selectedPolicy ? "active" : ""}" style="--tab-color:${strategy.color}" data-policy="${strategy.policy}" role="tab" aria-selected="${strategy.policy === selectedPolicy}">0${index + 1}</button>
  `).join("");
  document.querySelectorAll(".policy-tab").forEach((tab) => tab.addEventListener("click", () => selectPolicy(tab.dataset.policy)));
}

function selectPolicy(policy) {
  selectedPolicy = policy;
  renderCards();
  renderTabs();
  renderEvidence();
}

function renderEvidence() {
  const strategy = responseData.strategies.find((item) => item.policy === selectedPolicy);
  if (!strategy) return;
  const metrics = strategy.metrics;

  document.querySelector("#evidence-title").textContent = strategy.name;
  document.querySelector("#metric-final").textContent = compact.format(metrics.finalGoroutines);
  document.querySelector("#metric-final").style.color = strategy.color;
  document.querySelector("#metric-list").innerHTML = `
    <div><dt>PEAK GOROUTINES</dt><dd>${number.format(metrics.peakGoroutines)}</dd></div>
    <div><dt>BLOCKED SENDS</dt><dd>${number.format(metrics.blockedGoroutines)}</dd></div>
    <div><dt>WORKERS CANCELED</dt><dd>${number.format(metrics.canceledWorkers)}</dd></div>
    <div><dt>POST-TIMEOUT WORK</dt><dd>${formatDuration(metrics.postTimeoutWorkMs)}</dd></div>
    <div><dt>P99 QUEUE DELAY</dt><dd>${number.format(metrics.queueP99Ms)}ms</dd></div>
    <div><dt>SUCCESS RATE</dt><dd>${metrics.successPercent.toFixed(1)}%</dd></div>
  `;
  document.querySelector("#diagnostic-copy").textContent = diagnostics[strategy.policy];
  document.querySelector("#query-code").textContent = queries[strategy.policy];
  document.querySelector("#chart-note").textContent = strategy.tradeoff;
  renderEvents(strategy.events);
  drawChart(strategy);
}

function renderEvents(events) {
  document.querySelector("#event-rows").innerHTML = events.map((event) => `
    <tr>
      <td>${escapeHTML(event.requestId)}</td>
      <td class="event-${event.outcome}">${escapeHTML(event.outcome)}</td>
      <td>${escapeHTML(event.goroutineState)}</td>
      <td class="event-hash">${escapeHTML(event.stackHash)}</td>
      <td class="${event.contextCanceled ? "event-true" : ""}">${event.contextCanceled}</td>
      <td>${escapeHTML(event.resultDelivery)}</td>
      <td>${escapeHTML(event.poolSlot)}</td>
    </tr>
  `).join("");
}

function drawChart(strategy) {
  const canvas = document.querySelector("#timeline-chart");
  const rect = canvas.getBoundingClientRect();
  const ratio = window.devicePixelRatio || 1;
  canvas.width = Math.max(600, rect.width * ratio);
  canvas.height = rect.height * ratio;
  const context = canvas.getContext("2d");
  context.scale(ratio, ratio);

  const width = canvas.width / ratio;
  const height = canvas.height / ratio;
  const padding = { top: 18, right: 16, bottom: 28, left: 44 };
  const plotWidth = width - padding.left - padding.right;
  const plotHeight = height - padding.top - padding.bottom;
  const points = strategy.timeline;
  const maxActive = Math.max(1, ...points.map((point) => point.active + point.queued));
  const maxTimeout = Math.max(1, ...points.map((point) => point.timedOut));

  context.clearRect(0, 0, width, height);
  context.font = "9px SFMono-Regular, Menlo, monospace";
  context.fillStyle = "#68736d";
  context.strokeStyle = "#202825";
  context.lineWidth = 1;

  for (let line = 0; line <= 4; line++) {
    const y = padding.top + (plotHeight / 4) * line;
    context.beginPath();
    context.moveTo(padding.left, y);
    context.lineTo(width - padding.right, y);
    context.stroke();
    const label = Math.round(maxActive * (1 - line / 4));
    context.fillText(number.format(label), 4, y + 3);
  }

  ["0s", "30s", "60s", "90s"].forEach((label, index) => {
    const x = padding.left + plotWidth * (index / 3);
    context.fillText(label, x - (index === 0 ? 0 : 10), height - 7);
  });

  const xFor = (index) => padding.left + (index / Math.max(1, points.length - 1)) * plotWidth;
  const yActive = (value) => padding.top + plotHeight - (value / maxActive) * plotHeight;
  const yTimeout = (value) => padding.top + plotHeight - (value / maxTimeout) * plotHeight * .48;

  const gradient = context.createLinearGradient(0, padding.top, 0, padding.top + plotHeight);
  gradient.addColorStop(0, `${strategy.color}38`);
  gradient.addColorStop(1, `${strategy.color}00`);
  context.beginPath();
  points.forEach((point, index) => {
    const x = xFor(index);
    const y = yActive(point.active + point.queued);
    if (index === 0) context.moveTo(x, y);
    else context.lineTo(x, y);
  });
  context.lineTo(xFor(points.length - 1), padding.top + plotHeight);
  context.lineTo(xFor(0), padding.top + plotHeight);
  context.closePath();
  context.fillStyle = gradient;
  context.fill();

  drawLine(context, points, (point) => point.active + point.queued, xFor, yActive, strategy.color, 2);
  drawLine(context, points, (point) => point.timedOut, xFor, yTimeout, "#ff6b5e", 1.5, [4, 4]);
}

function drawLine(context, points, value, xFor, yFor, color, width, dash = []) {
  context.beginPath();
  context.setLineDash(dash);
  context.strokeStyle = color;
  context.lineWidth = width;
  points.forEach((point, index) => {
    const x = xFor(index);
    const y = yFor(value(point));
    if (index === 0) context.moveTo(x, y);
    else context.lineTo(x, y);
  });
  context.stroke();
  context.setLineDash([]);
}

function formatDuration(milliseconds) {
  if (milliseconds < 1000) return `${number.format(milliseconds)}ms`;
  const seconds = milliseconds / 1000;
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
  return `${(seconds / 60).toFixed(1)}m`;
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

Object.entries(controls).forEach(([, input]) => {
  input.addEventListener("input", () => {
    syncControls();
    document.querySelectorAll(".preset").forEach((button) => button.classList.remove("active"));
  });
});

document.querySelectorAll(".preset").forEach((button) => {
  button.addEventListener("click", () => {
    Object.entries(presets[button.dataset.preset]).forEach(([key, value]) => { controls[key].value = value; });
    document.querySelectorAll(".preset").forEach((item) => item.classList.toggle("active", item === button));
    syncControls();
    runSimulation(configFromControls());
  });
});

document.querySelector("#controls").addEventListener("submit", (event) => {
  event.preventDefault();
  runSimulation(configFromControls());
});

document.querySelector("#copy-query").addEventListener("click", async () => {
  const button = document.querySelector("#copy-query");
  try {
    await navigator.clipboard.writeText(document.querySelector("#query-code").textContent);
    button.textContent = "COPIED";
    setTimeout(() => { button.textContent = "COPY"; }, 1200);
  } catch {
    button.textContent = "SELECT QUERY";
  }
});

window.addEventListener("resize", () => {
  if (responseData) renderEvidence();
});

syncControls();
runSimulation();
