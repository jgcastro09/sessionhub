import { escapeHtml, formatDate, rolePill, statusPill } from "./components.js";
import { GraphView } from "./graph-view.js";
import {
  formatBytes, installRegistryData, registryState, routeFor, searchRegistry,
} from "./state.js";
import { renderView } from "./views.js";

const elements = {
  main: document.getElementById("main-content"),
  loading: document.getElementById("loading"),
  generatedAt: document.getElementById("generated-at"),
  reload: document.getElementById("reload-data"),
  inspector: document.getElementById("inspector"),
  inspectorType: document.getElementById("inspector-type"),
  inspectorContent: document.getElementById("inspector-content"),
  closeInspector: document.getElementById("close-inspector"),
  search: document.getElementById("global-search"),
  searchPanel: document.getElementById("search-panel"),
  searchTitle: document.getElementById("search-title"),
  searchResults: document.getElementById("search-results"),
  closeSearch: document.getElementById("close-search"),
  pendingBadge: document.getElementById("pending-badge"),
  uncategorizedBadge: document.getElementById("uncategorized-badge"),
  sidebarStatus: document.getElementById("sidebar-status"),
  toast: document.getElementById("toast"),
};

let graphView = null;
let searchTimer = null;
let toastTimer = null;

function parseRoute() {
  const hash = location.hash.replace(/^#/, "") || "overview";
  const slash = hash.indexOf("/");
  return slash < 0
    ? { view: hash, id: "" }
    : { view: hash.slice(0, slash), id: decodeURIComponent(hash.slice(slash + 1)) };
}

function showToast(message) {
  clearTimeout(toastTimer);
  elements.toast.textContent = message;
  elements.toast.classList.add("visible");
  toastTimer = setTimeout(() => elements.toast.classList.remove("visible"), 2600);
}

function copyText(value, label = "Path") {
  navigator.clipboard.writeText(value).then(
    () => showToast(`${label} copied.`),
    () => showToast("Clipboard access was unavailable."),
  );
}

function listSection(title, values, renderer = (value) => escapeHtml(value)) {
  if (!values?.length) return "";
  return `<section class="inspector-section"><h3>${escapeHtml(title)}</h3><div class="inspector-links">${values.map(renderer).join("")}</div></section>`;
}

function pathLink(path) {
  return `<button class="inspector-link inspect-from-inspector" data-type="file" data-id="${escapeHtml(path)}" type="button"><span>${escapeHtml(path.split("/").at(-1))}</span><small>${escapeHtml(path)}</small></button>`;
}

function fileInspector(file) {
  const areas = file.productAreaIds.map((id) => registryState.areasById.get(id)).filter(Boolean);
  const symbols = Object.entries(file.symbols || {}).filter(([, values]) => values.length);
  return `<div class="inspector-title"><h2>${escapeHtml(file.filename)}</h2>${statusPill(file.reviewStatus)}</div>
    <p class="inspector-description">${escapeHtml(file.description)}</p>
    <div class="path-box"><code>${escapeHtml(file.path)}</code><button class="copy-value" data-copy="${escapeHtml(file.path)}" type="button">Copy</button></div>
    <dl class="metadata-grid">
      <div><dt>Module</dt><dd>${escapeHtml(file.module)}</dd></div>
      <div><dt>Role</dt><dd>${rolePill(file.architecturalRole)}</dd></div>
      <div><dt>Language</dt><dd>${escapeHtml(file.language)}</dd></div>
      <div><dt>Kind</dt><dd>${escapeHtml(file.kind)}</dd></div>
      <div><dt>Lines</dt><dd>${file.lineCount.toLocaleString()}</dd></div>
      <div><dt>Size</dt><dd>${formatBytes(file.sizeBytes)}</dd></div>
      <div><dt>Criticality</dt><dd>${escapeHtml(file.criticality)}</dd></div>
      <div><dt>Last modified</dt><dd>${escapeHtml(formatDate(file.lastModifiedAt))}</dd></div>
      <div><dt>Last scan</dt><dd>${escapeHtml(formatDate(file.lastScannedAt))}</dd></div>
    </dl>
    ${listSection("Responsibilities", file.responsibilities, (value) => `<span class="inspector-bullet">${escapeHtml(value)}</span>`)}
    ${listSection("Product Areas", areas, (area) => `<a class="inspector-tag" href="${routeFor("area", area.id)}" style="--area-color:${escapeHtml(area.color)}">${escapeHtml(area.label)}</a>`)}
    ${listSection("Confirmed related files", file.relatedFiles, pathLink)}
    ${listSection("Includes", file.includes?.filter((value) => !["algorithm", "array", "atomic", "chrono", "cmath", "cstdint", "filesystem", "functional", "map", "memory", "mutex", "optional", "set", "string", "thread", "unordered_map", "utility", "vector"].includes(value)).slice(0, 30), (value) => `<span class="code-chip">${escapeHtml(value)}</span>`)}
    ${listSection("Imports", file.imports?.slice(0, 30), (value) => `<span class="code-chip">${escapeHtml(value)}</span>`)}
    ${symbols.length ? `<section class="inspector-section"><h3>Symbols</h3>${symbols.map(([type, values]) => `<details><summary>${escapeHtml(type)} <span>${values.length}</span></summary><div class="symbol-list">${values.slice(0, 50).map((value) => `<code>${escapeHtml(value)}</code>`).join("")}</div></details>`).join("")}</section>` : ""}
    <section class="inspector-section"><h3>Integrity</h3><div class="hash-block"><span>SHA-256</span><code>${escapeHtml(file.hash)}</code></div></section>
    <a class="button primary full-width" href="#code-reader/${encodeURIComponent(file.path)}">Open in Code Reader</a>
    <a class="button secondary full-width" href="#relationships/${encodeURIComponent(`file:${file.path}`)}">Explore relationships</a>`;
}

function unitInspector(unit) {
  return `<div class="inspector-title"><h2>${escapeHtml(unit.label)}</h2>${statusPill(unit.reviewStatus)}</div>
    <p class="inspector-description">${escapeHtml(unit.description)}</p>
    <dl class="metadata-grid"><div><dt>Module</dt><dd>${escapeHtml(unit.module)}</dd></div><div><dt>Role</dt><dd>${rolePill(unit.role)}</dd></div><div><dt>Files</dt><dd>${unit.filePaths.length}</dd></div><div><dt>Lines</dt><dd>${unit.lineCount.toLocaleString()}</dd></div><div><dt>Criticality</dt><dd>${escapeHtml(unit.criticality)}</dd></div><div><dt>Language</dt><dd>${escapeHtml(unit.language)}</dd></div></dl>
    ${listSection("Source files", unit.filePaths, pathLink)}
    <a class="button primary full-width" href="#relationships/${encodeURIComponent(unit.id)}">Explore relationships</a>`;
}

function areaInspector(area) {
  const children = registryState.data.productAreas.filter((item) => item.parentId === area.id);
  return `<div class="inspector-title"><h2>${escapeHtml(area.label)}</h2><span class="area-dot" style="--area-color:${escapeHtml(area.color)}"></span></div>
    <p class="inspector-description">${escapeHtml(area.description)}</p>
    <dl class="metadata-grid"><div><dt>Logical units</dt><dd>${area.counts.units}</dd></div><div><dt>Files</dt><dd>${area.counts.files}</dd></div><div><dt>Modules</dt><dd>${Object.keys(area.counts.modules).length}</dd></div><div><dt>Pending</dt><dd>${area.counts.pending}</dd></div></dl>
    ${listSection("Subareas", children, (child) => `<a class="inspector-link" href="${routeFor("area", child.id)}"><span>${escapeHtml(child.label)}</span><small>${child.counts.units} units</small></a>`)}
    ${listSection("Architecture roles", Object.entries(area.counts.roles), ([role, count]) => `<span class="inspector-count">${rolePill(role)}<strong>${count}</strong></span>`)}
    ${listSection("Modules", Object.entries(area.counts.modules).slice(0, 30), ([module, count]) => `<a class="inspector-count" href="#architecture/${encodeURIComponent(module)}"><span>${escapeHtml(module)}</span><strong>${count}</strong></a>`)}
    <a class="button primary full-width" href="${routeFor("area", area.id)}">Open Product Area</a>`;
}

function moduleInspector(module) {
  const units = module.unitIds.map((id) => registryState.unitsById.get(id)).filter(Boolean);
  return `<div class="inspector-title"><h2>${escapeHtml(module.path)}</h2></div>
    <p class="inspector-description">Physical architecture owner containing ${module.unitCount} logical units.</p>
    <dl class="metadata-grid"><div><dt>All units</dt><dd>${module.unitCount}</dd></div><div><dt>Direct units</dt><dd>${module.directUnitIds.length}</dd></div><div><dt>Files</dt><dd>${units.reduce((sum, unit) => sum + unit.filePaths.length, 0)}</dd></div></dl>
    ${listSection("Direct logical units", module.directUnitIds.map((id) => registryState.unitsById.get(id)).filter(Boolean).slice(0, 40), (unit) => `<button class="inspector-link inspect-from-inspector" data-type="unit" data-id="${escapeHtml(unit.id)}" type="button"><span>${escapeHtml(unit.label)}</span><small>${escapeHtml(unit.description)}</small></button>`)}
    <a class="button primary full-width" href="#architecture/${encodeURIComponent(module.path)}">Open architecture module</a>`;
}

function bindInspectorContent() {
  elements.inspectorContent.querySelectorAll(".copy-value").forEach((button) => button.addEventListener("click", () => copyText(button.dataset.copy)));
  elements.inspectorContent.querySelectorAll(".inspect-from-inspector").forEach((button) => button.addEventListener("click", () => openInspector(button.dataset.type, button.dataset.id)));
}

function openInspector(type, id) {
  let html = "";
  let label = type;
  if (type === "file") {
    const file = registryState.filesByPath.get(id);
    if (!file) return;
    html = fileInspector(file); label = "Source file";
  } else if (type === "unit") {
    const unit = registryState.unitsById.get(id);
    if (!unit) return;
    html = unitInspector(unit); label = "Logical unit";
  } else if (type === "area") {
    const area = registryState.areasById.get(id);
    if (!area) return;
    html = areaInspector(area); label = "Product area";
  } else if (type === "module") {
    const module = registryState.modulesById.get(id) || registryState.modulesByPath.get(id);
    if (!module) return;
    html = moduleInspector(module); label = "Architecture module";
  } else return;
  elements.inspectorType.textContent = label;
  elements.inspectorContent.innerHTML = html;
  elements.inspector.classList.add("open");
  elements.inspector.setAttribute("aria-hidden", "false");
  document.body.classList.add("inspector-open");
  bindInspectorContent();
}

function closeInspector() {
  elements.inspector.classList.remove("open");
  elements.inspector.setAttribute("aria-hidden", "true");
  document.body.classList.remove("inspector-open");
}

function bindGraph(context) {
  const canvas = document.getElementById("graph-canvas");
  if (!canvas || !context) return;
  graphView = new GraphView(canvas, openInspector, showToast);
  graphView.context = context;
  graphView.render();
  const label = document.getElementById("graph-context-label");
  if (label) label.textContent = context.label;
  document.getElementById("graph-layout")?.addEventListener("change", (event) => graphView.setOptions({ layout: event.target.value }));
  document.getElementById("graph-depth")?.addEventListener("change", (event) => graphView.setOptions({ depth: event.target.value }));
  document.getElementById("graph-limit")?.addEventListener("change", (event) => graphView.setOptions({ limit: event.target.value }));
  document.getElementById("graph-fit")?.addEventListener("click", () => graphView.fit());
  document.getElementById("graph-focus")?.addEventListener("click", () => graphView.focusSelection());
}

function updateNavigation(route) {
  document.querySelectorAll("#primary-nav a").forEach((link) => link.classList.toggle("active", link.dataset.view === route.view));
}

function renderRoute() {
  if (!registryState.data) return;
  graphView?.destroy(); graphView = null;
  closeSearchPanel();
  const route = parseRoute();
  updateNavigation(route);
  const result = renderView(elements.main, route, openInspector);
  bindGraph(result.graphContext);
  if (route.view === "code-reader") bindCodeReader(route.id);
  if (route.view === "global-code-search") bindGlobalCodeSearch();
  if (route.view === "git-audit") bindGitAudit();
  elements.main.scrollTop = 0;
  elements.main.focus({ preventScroll: true });
}

function renderSource(content, source) {
  const lines = source.split("\n");
  content.innerHTML = lines.map((line, index) => `<div class="code-line"><span>${index + 1}</span><code>${escapeHtml(line || " ")}</code></div>`).join("");
}

function renderDiff(content, diff) {
  content.innerHTML = `<pre class="code-diff">${escapeHtml(diff || "No changes for this file.")}</pre>`;
}

async function bindCodeReader(path) {
  const content = document.getElementById("code-reader-content");
  const metadata = document.getElementById("code-reader-meta");
  const version = document.getElementById("code-reader-version");
  const diffButton = document.getElementById("code-reader-diff");
  if (!content || !metadata) return;
  const selectedPath = registryState.filesByPath.has(path) ? path : registryState.data.files[0]?.path;
  if (!selectedPath) return;
  const loadSource = async () => {
    const ref = version?.value || "working";
    try {
      const endpoint = ref === "working"
        ? `/__registry/source?path=${encodeURIComponent(selectedPath)}`
        : `/__registry/git-source?path=${encodeURIComponent(selectedPath)}&ref=${encodeURIComponent(ref)}`;
      const response = await fetch(endpoint, { cache: "no-store" });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const source = await response.json();
      metadata.textContent = ref === "working"
        ? `${source.language} · Working tree · Modified ${formatDate(source.lastModifiedAt)}`
        : `Git ${source.ref}`;
      renderSource(content, source.content);
      if (diffButton) diffButton.textContent = ref === "working" ? "View Git diff" : "View diff vs parent";
    } catch (error) {
      metadata.textContent = "Source unavailable";
      content.textContent = `Unable to load this source version: ${error.message}`;
    }
  };
  try {
    const response = await fetch(`/__registry/git-history?path=${encodeURIComponent(selectedPath)}`, { cache: "no-store" });
    if (response.ok && version) {
      const history = await response.json();
      history.commits.forEach((commit) => version.insertAdjacentHTML("beforeend", `<option value="${escapeHtml(commit.hash)}">${escapeHtml(commit.shortHash)} · ${escapeHtml(commit.subject)}</option>`));
    }
  } catch { /* Git history is optional when browsing source. */ }
  version?.addEventListener("change", loadSource);
  diffButton?.addEventListener("click", async () => {
    const ref = version?.value || "working";
    try {
      const suffix = ref === "working" ? "mode=working" : `ref=${encodeURIComponent(ref)}`;
      const response = await fetch(`/__registry/git-diff?path=${encodeURIComponent(selectedPath)}&${suffix}`, { cache: "no-store" });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const diff = await response.json();
      metadata.textContent = diff.available ? `Diff: ${diff.base} → ${diff.target}` : (diff.reason || "No Git diff available.");
      renderDiff(content, diff.diff);
    } catch (error) {
      metadata.textContent = "Diff unavailable";
      content.textContent = `Unable to load Git diff: ${error.message}`;
    }
  });
  await loadSource();
}

function renderSearchResults(query) {
  const results = searchRegistry(query);
  elements.searchPanel.hidden = false;
  elements.searchTitle.textContent = `${results.length} results for “${query}”`;
  elements.searchResults.innerHTML = results.length ? results.map((result) => `<button class="search-result" data-type="${escapeHtml(result.type)}" data-id="${escapeHtml(result.id)}" type="button"><span class="search-type">${escapeHtml(result.type)}</span><span><strong>${escapeHtml(result.label)}</strong><small>${escapeHtml(result.subtitle)}</small><code>${escapeHtml(result.path)}</code></span></button>`).join("") : `<div class="search-empty">No registry entries match this query.</div>`;
  elements.searchResults.querySelectorAll(".search-result").forEach((button) => button.addEventListener("click", () => {
    if (["area", "module"].includes(button.dataset.type)) location.hash = routeFor(button.dataset.type, button.dataset.id);
    else openInspector(button.dataset.type, button.dataset.id);
    closeSearchPanel();
  }));
}

function closeSearchPanel() {
  elements.searchPanel.hidden = true;
}

function bindGlobalCodeSearch() {
  const input = document.getElementById("source-search-query");
  const count = document.getElementById("source-search-count");
  const results = document.getElementById("source-search-results");
  if (!input || !count || !results) return;
  let timer = null;
  const search = async () => {
    const query = input.value.trim();
    if (!query) {
      count.textContent = "Enter a word or phrase to search all registered code.";
      results.innerHTML = "";
      return;
    }
    count.textContent = "Searching registered source files…";
    try {
      const response = await fetch(`/__registry/text-search?query=${encodeURIComponent(query)}`, { cache: "no-store" });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const payload = await response.json();
      const occurrences = payload.files.reduce((total, file) => total + file.occurrences, 0);
      count.textContent = `${occurrences} occurrences in ${payload.files.length} files${payload.files.length === 100 ? " (result limit reached)" : ""}.`;
      results.innerHTML = payload.files.length ? payload.files.map((file) => `<article class="source-search-result"><header><div><strong>${escapeHtml(file.path)}</strong><small>${escapeHtml(file.language)}</small></div><span>${file.occurrences} occurrences</span></header><div>${file.matches.map((match) => `<button type="button" data-path="${escapeHtml(file.path)}"><code>${match.line}</code><span>${escapeHtml(match.text || " ")}</span></button>`).join("")}</div></article>`).join("") : `<div class="empty-state"><span>⌕</span><h3>No source matches</h3><p>Try a different word, phrase, or spelling.</p></div>`;
      results.querySelectorAll("button[data-path]").forEach((button) => button.addEventListener("click", () => { location.hash = `#code-reader/${encodeURIComponent(button.dataset.path)}`; }));
    } catch (error) {
      count.textContent = "Source search is unavailable.";
      results.innerHTML = `<div class="empty-state"><span>!</span><h3>Search failed</h3><p>${escapeHtml(error.message)}</p></div>`;
    }
  };
  input.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(search, 180); });
  input.focus();
}

function gitChangeRow(change) {
  const flags = [change.staged === "true" ? "staged" : "", change.worktree === "true" ? "working" : "", change.untracked === "true" ? "untracked" : "", change.conflicted === "true" ? "conflict" : ""].filter(Boolean).join(" · ");
  return `<button class="git-change-row" type="button" data-path="${escapeHtml(change.path)}"><code>${escapeHtml(change.status)}</code><span><strong>${escapeHtml(change.path)}</strong><small>${escapeHtml(flags || "tracked")}</small></span></button>`;
}

function renderGitAudit(audit) {
  const container = document.getElementById("git-audit-content");
  if (!container) return;
  if (!audit.available) {
    container.innerHTML = `<div class="empty-state"><span>!</span><h3>Git audit unavailable</h3><p>${escapeHtml(audit.reason || "Git state could not be read.")}</p></div>`;
    return;
  }
  const sync = audit.behind ? "Remote changes need integration" : audit.ahead ? "Local commits need push" : "Synchronized with upstream";
  const changed = audit.changes || [];
  const registered = audit.registry?.changedRegisteredFiles || [];
  const pending = audit.registry?.pendingSemanticReview || [];
  container.innerHTML = `<section class="metric-grid git-metrics">
      <article class="metric-card blue"><span>Branch</span><strong>${escapeHtml(audit.branch)}</strong><small>${escapeHtml(audit.upstream || "No upstream")}</small></article>
      <article class="metric-card purple"><span>Remote state</span><strong>${audit.ahead} ↑ ${audit.behind} ↓</strong><small>${escapeHtml(sync)}</small></article>
      <article class="metric-card ${changed.length ? "warning" : "green"}"><span>Local changes</span><strong>${changed.length}</strong><small>${audit.conflicts.length} conflicts</small></article>
      <article class="metric-card ${pending.length ? "warning" : "green"}"><span>Registry review</span><strong>${pending.length}</strong><small>${registered.length} changed registered files</small></article>
    </section>
    <div class="dashboard-grid git-audit-grid">
      <section class="panel"><div class="panel-heading"><div><span class="eyebrow">Current commit</span><h2>${escapeHtml(audit.head?.shortHash || "No commit")}</h2></div></div><p class="git-subject">${escapeHtml(audit.head?.subject || "Repository has no commits.")}</p><dl class="git-details"><div><dt>Author</dt><dd>${escapeHtml(audit.head?.author || "—")}</dd></div><div><dt>Date</dt><dd>${escapeHtml(formatDate(audit.head?.date || ""))}</dd></div><div><dt>Remote</dt><dd>${escapeHtml(audit.remote || "Not configured")}</dd></div></dl></section>
      <section class="panel"><div class="panel-heading"><div><span class="eyebrow">Registry correlation</span><h2>Working tree vs Registry</h2></div></div><div class="git-correlation"><div><strong>${registered.length}</strong><span>changed registered files</span></div><div><strong>${pending.length}</strong><span>pending semantic reviews</span></div><div><strong>${audit.conflicts.length}</strong><span>Git conflicts</span></div></div>${pending.length ? `<p class="git-warning">Reload data and complete semantic review before committing registered code.</p>` : `<p class="git-ok">Registry review is aligned with the current snapshot.</p>`}</section>
      <section class="panel span-2"><div class="panel-heading"><div><span class="eyebrow">Local working tree</span><h2>${changed.length ? `${changed.length} changes to inspect` : "Working tree is clean"}</h2></div></div>${changed.length ? `<div class="git-change-list">${changed.map(gitChangeRow).join("")}</div>` : `<p class="git-ok">No modified, staged, removed, or untracked files.</p>`}</section>
      <section class="panel span-2"><div class="panel-heading"><div><span class="eyebrow">Version history</span><h2>Recent commits</h2></div></div><div class="git-commit-list">${(audit.recentCommits || []).map((commit) => `<div><code>${escapeHtml(commit.shortHash)}</code><span><strong>${escapeHtml(commit.subject)}</strong><small>${escapeHtml(commit.author)} · ${escapeHtml(formatDate(commit.date))}</small></span></div>`).join("") || "<p>No commits available.</p>"}</div></section>
    </div>`;
  container.querySelectorAll(".git-change-row").forEach((button) => button.addEventListener("click", () => {
    if (registryState.filesByPath.has(button.dataset.path)) location.hash = `#code-reader/${encodeURIComponent(button.dataset.path)}`;
    else showToast("This path is not a registered source file.");
  }));
}

function bindGitAudit() {
  const refresh = document.getElementById("git-audit-refresh");
  const fetchRemote = document.getElementById("git-audit-fetch");
  const loadAudit = async (remote = false) => {
    refresh.disabled = true;
    fetchRemote.disabled = true;
    try {
      const response = await fetch(remote ? "/__registry/git-fetch" : "/__registry/git-audit", { method: remote ? "POST" : "GET", cache: "no-store" });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      renderGitAudit(await response.json());
      if (remote) showToast("Git remote checked without changing source files.");
    } catch (error) {
      const container = document.getElementById("git-audit-content");
      if (container) container.innerHTML = `<div class="empty-state"><span>!</span><h3>Git audit failed</h3><p>${escapeHtml(error.message)}</p></div>`;
    } finally {
      refresh.disabled = false;
      fetchRemote.disabled = false;
    }
  };
  refresh.addEventListener("click", () => loadAudit());
  fetchRemote.addEventListener("click", () => loadAudit(true));
  loadAudit();
}

function updateChrome() {
  const { data } = registryState;
  elements.generatedAt.textContent = `Generated ${formatDate(data.generatedAt)}`;
  elements.pendingBadge.textContent = data.audit.counts.pending;
  elements.pendingBadge.classList.toggle("visible", data.audit.counts.pending > 0);
  const uncategorized = data.files.filter((file) => file.category === "uncategorized" || !file.productAreaIds?.length).length;
  elements.uncategorizedBadge.textContent = uncategorized;
  elements.uncategorizedBadge.classList.toggle("visible", uncategorized > 0);
  elements.sidebarStatus.innerHTML = `<span class="status-light ${data.audit.status}"></span><div><strong>${data.audit.status === "approved" ? "Registry approved" : "Attention required"}</strong><small>${data.counts.total} files · ${data.logicalUnits.length} units</small></div>`;
}

function validatePayload(data) {
  const requiredArrays = ["files", "logicalUnits", "modules", "productAreas", "architectureTree"];
  if (!data || data.explorerSchemaVersion !== 1 || requiredArrays.some((key) => !Array.isArray(data[key])) || !data.graph?.nodes || !data.graph?.edges || !data.productAreaCoverage) {
    throw new Error("site-data.json does not contain a supported Explorer payload.");
  }
}

async function loadData({ announce = false } = {}) {
  elements.loading.classList.add("visible");
  elements.reload.disabled = true;
  try {
    const response = await fetch(`../generated/site-data.json?refresh=${Date.now()}`, { cache: "no-store" });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const data = await response.json();
    validatePayload(data);
    installRegistryData(data);
    updateChrome();
    renderRoute();
    if (announce) showToast(`Registry reloaded: ${data.counts.total} files.`);
  } catch (error) {
    elements.main.innerHTML = `<div class="fatal-error"><span>!</span><h1>Unable to load Code Registry data</h1><p>${escapeHtml(error.message)}</p><code>python "Code Registry/tools/code_registry.py" full</code><p>Then reload this page through the local Explorer server.</p></div>`;
  } finally {
    elements.loading.classList.remove("visible");
    elements.reload.disabled = false;
  }
}

async function reloadRegistry() {
  elements.loading.classList.add("visible");
  elements.reload.disabled = true;
  try {
    const response = await fetch("/__registry/reload", { method: "POST", cache: "no-store" });
    const result = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(result.message || `HTTP ${response.status}`);
    await loadData();
    const events = result.events || {};
    showToast(`Registry synchronized: ${result.counts?.registered ?? 0} files · ${events.new?.length ?? 0} new · ${events.removed?.length ?? 0} removed.`);
  } catch (error) {
    elements.main.innerHTML = `<div class="fatal-error"><span>!</span><h1>Unable to synchronize Code Registry</h1><p>${escapeHtml(error.message)}</p><code>python "Code Registry/tools/code_registry.py" full</code></div>`;
  } finally {
    elements.loading.classList.remove("visible");
    elements.reload.disabled = false;
  }
}

elements.reload.addEventListener("click", reloadRegistry);
elements.closeInspector.addEventListener("click", closeInspector);
elements.closeSearch.addEventListener("click", closeSearchPanel);
elements.search.addEventListener("input", () => {
  clearTimeout(searchTimer);
  const query = elements.search.value.trim();
  if (query.length < 2) { closeSearchPanel(); return; }
  searchTimer = setTimeout(() => renderSearchResults(query), 90);
});
elements.main.addEventListener("registry:rebind", (event) => {
  event.detail.querySelectorAll(".inspect-trigger").forEach((button) => button.addEventListener("click", () => openInspector(button.dataset.entityType, button.dataset.entityId)));
});
window.addEventListener("hashchange", renderRoute);
document.addEventListener("keydown", (event) => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLocaleLowerCase() === "k") {
    event.preventDefault(); elements.search.focus(); elements.search.select();
  }
  if (event.key === "Escape") {
    if (!elements.searchPanel.hidden) closeSearchPanel();
    else closeInspector();
  }
});

loadData();
