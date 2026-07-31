import {
  architectureTree, areaCard, barList, bindInspectTriggers, emptyState, escapeHtml,
  fileRow, formatDate, graphToolbar, metricCard, rolePill, statusPill, unitRow,
} from "./components.js";
import { registryState, roleInfo } from "./state.js";

function pageHeader(eyebrow, title, description, actions = "") {
  return `<header class="page-header">
    <div><span class="eyebrow">${escapeHtml(eyebrow)}</span><h1>${escapeHtml(title)}</h1><p>${escapeHtml(description)}</p></div>
    ${actions ? `<div class="page-actions">${actions}</div>` : ""}
  </header>`;
}

function overviewView() {
  const { data } = registryState;
  const audit = data.audit;
  const counts = data.counts;
  const topAreas = data.productAreaTree;
  const graph = data.graph.counts;
  const auditTone = audit.status === "approved" ? "approved" : "failed";
  return `${pageHeader("Repository intelligence", "Architecture at a glance", "Navigate ownership, product domains, logical units and reviewed source relationships without opening the full registry.")}
    <section class="audit-banner ${auditTone}">
      <span class="audit-icon">${audit.status === "approved" ? "✓" : "!"}</span>
      <div><strong>${audit.status === "approved" ? "Registry approved" : "Registry requires attention"}</strong>
      <p>${counts.total} maintained files · generated ${escapeHtml(formatDate(data.generatedAt))}</p></div>
      ${statusPill(audit.status === "approved" ? "reviewed" : "needs_review")}
    </section>
    <section class="metric-grid">
      ${metricCard("Maintained files", counts.total, `${data.logicalUnits.length} logical units`, "blue")}
      ${metricCard("Architecture modules", data.modules.length, `${Object.keys(counts.modules).length} owned module paths`, "purple")}
      ${metricCard("Product areas", topAreas.length, `${data.productAreaCoverage.mappedFiles}/${counts.total} files classified`, "cyan")}
      ${metricCard("Code lines", counts.lines.toLocaleString(), `${counts.total} maintained files`, "orange")}
      ${metricCard("Relationships", graph.edges, `${graph.nodes} graph nodes`, "orange")}
      ${metricCard("Needs review", audit.counts.pending, audit.counts.pending ? "Semantic confirmation required" : "All hashes reviewed", audit.counts.pending ? "warning" : "green")}
    </section>
    <div class="dashboard-grid">
      <section class="panel span-2"><div class="panel-heading"><div><span class="eyebrow">Browse by purpose</span><h2>Product Areas</h2></div><a href="#product-areas">View all</a></div>
        <div class="area-grid compact">${topAreas.slice(0, 8).map(areaCard).join("")}</div>
      </section>
      <section class="panel"><div class="panel-heading"><div><span class="eyebrow">Code families</span><h2>Language coverage</h2></div></div>
        <div class="bar-list">${barList(counts.families, counts.total, 8)}</div>
      </section>
      <section class="panel"><div class="panel-heading"><div><span class="eyebrow">Ownership</span><h2>Largest modules</h2></div><a href="#architecture">Explore</a></div>
        <div class="bar-list">${barList(counts.modules, counts.total, 10)}</div>
      </section>
      <section class="panel span-2"><div class="panel-heading"><div><span class="eyebrow">Graph model</span><h2>Relationship inventory</h2></div><a href="#relationships">Open graph</a></div>
        <div class="relation-summary">${Object.entries(graph.edgeTypes).map(([type, value]) => `<div><strong>${value}</strong><span>${escapeHtml(type)}</span></div>`).join("")}</div>
      </section>
    </div>`;
}

function architectureView(modulePath) {
  const { data } = registryState;
  if (!modulePath) {
    return `${pageHeader("Physical ownership", "Architecture", "The module tree answers where code belongs and which owner is responsible for its durable state and behavior.")}
      <div class="split-view">
        <section class="panel architecture-tree"><div class="panel-heading"><div><span class="eyebrow">${data.modules.length} modules</span><h2>Ownership tree</h2></div></div><ul>${architectureTree(data.architectureTree)}</ul></section>
        <section class="panel architecture-guide"><span class="guide-symbol">⌘</span><h2>Select a module</h2><p>Open a branch to inspect its logical units, files, responsibilities and local dependency graph.</p>
          <dl><div><dt>Module</dt><dd>Physical ownership</dd></div><div><dt>Product Area</dt><dd>Cross-cutting feature membership</dd></div><div><dt>Logical Unit</dt><dd>Related source/header pair</dd></div></dl>
        </section>
      </div>`;
  }
  const module = registryState.modulesByPath.get(modulePath);
  if (!module) return `${pageHeader("Physical ownership", "Module not found", modulePath)}${emptyState("Unknown module", "Return to the architecture tree and select an existing owner.")}`;
  const units = module.unitIds.map((id) => registryState.unitsById.get(id)).filter(Boolean);
  return `${pageHeader("Architecture module", module.path, `${module.unitCount} logical units owned by this architecture branch.`, `<a class="button secondary" href="#relationships/${encodeURIComponent(module.id)}">Open graph</a>`) }
    <div class="detail-metrics">${metricCard("Logical units", module.unitCount)}${metricCard("Direct units", module.directUnitIds.length)}${metricCard("Source files", units.reduce((sum, unit) => sum + unit.filePaths.length, 0))}</div>
    <section class="panel"><div class="panel-heading"><div><span class="eyebrow">Owned implementation</span><h2>Logical units</h2></div></div><div class="unit-list">${units.map((unit) => unitRow(unit)).join("")}</div></section>`;
}

function areaRoleSections(area) {
  const units = area.unitIds.map((id) => registryState.unitsById.get(id)).filter(Boolean);
  const byRole = new Map();
  units.forEach((unit) => {
    if (!byRole.has(unit.role)) byRole.set(unit.role, []);
    byRole.get(unit.role).push(unit);
  });
  return [...byRole.entries()]
    .sort((a, b) => b[1].length - a[1].length || roleInfo(a[0]).label.localeCompare(roleInfo(b[0]).label))
    .map(([role, roleUnits]) => `<section class="role-section"><div class="role-heading">${rolePill(role)}<span>${roleUnits.length} units</span></div><div class="unit-list">${roleUnits.map((unit) => unitRow(unit)).join("")}</div></section>`)
    .join("");
}

function productAreasView(areaId) {
  const { data } = registryState;
  if (!areaId) {
    return `${pageHeader("Cross-cutting architecture", "Product Areas", "Follow a feature across coordinators, state, UI, services, rendering and persistence—even when those files live in different modules.")}
      <div class="area-grid">${data.productAreaTree.map(areaCard).join("")}</div>`;
  }
  const area = registryState.areasById.get(areaId);
  if (!area) return `${pageHeader("Product Areas", "Area not found", areaId)}${emptyState("Unknown product area", "Return to Product Areas and select an existing feature.")}`;
  const children = data.productAreas.filter((item) => item.parentId === area.id);
  return `${pageHeader("Product Area", area.label, area.description, `<a class="button primary" href="#relationships/${encodeURIComponent(`area:${area.id}`)}">Explore relationships</a>`) }
    <section class="feature-hero" style="--area-color:${escapeHtml(area.color)}">
      <div><span class="feature-symbol">◇</span><div><span class="eyebrow">Feature coverage</span><strong>${area.counts.units} logical units across ${Object.keys(area.counts.modules).length} modules</strong></div></div>
      <div class="feature-stats"><span><strong>${area.counts.files}</strong> files</span><span><strong>${area.counts.pending}</strong> pending</span><span><strong>${Object.keys(area.counts.roles).length}</strong> architecture roles</span></div>
    </section>
    ${children.length ? `<section><div class="section-heading"><div><span class="eyebrow">Feature map</span><h2>Subareas</h2></div></div><div class="area-grid compact">${children.map(areaCard).join("")}</div></section>` : ""}
    <div class="feature-layout">
      <div><div class="section-heading"><div><span class="eyebrow">Layered view</span><h2>Implementation by role</h2></div></div>${areaRoleSections(area)}</div>
      <aside class="panel sticky-summary"><div class="panel-heading"><div><span class="eyebrow">Distribution</span><h2>Modules</h2></div></div><div class="bar-list">${barList(area.counts.modules, area.counts.files, 15)}</div></aside>
    </div>`;
}

function nodesView(areaId) {
  const nodes = registryState.areasById.get("nodes");
  const categories = registryState.data.productAreas.filter((area) => area.parentId === "nodes");
  if (!areaId || areaId === "nodes") {
    return `${pageHeader("Registry-driven library", "Nodes", "Browse every generator, effect, input, output and graph infrastructure unit using the same categories represented by the source architecture.")}
      <section class="node-overview"><div><strong>${nodes.counts.units}</strong><span>logical units</span></div><div><strong>${nodes.counts.files}</strong><span>source files</span></div><div><strong>${categories.length}</strong><span>functional groups</span></div></section>
      <div class="area-grid">${categories.map((area) => `<a class="area-card" href="#nodes/${encodeURIComponent(area.id)}" style="--area-color:${escapeHtml(area.color)}"><div class="area-card-top"><span class="area-symbol">⬡</span></div><h3>${escapeHtml(area.label)}</h3><p>${escapeHtml(area.description)}</p><footer><strong>${area.counts.units}</strong> units <span>·</span> <strong>${area.counts.files}</strong> files</footer></a>`).join("")}</div>`;
  }
  const area = registryState.areasById.get(areaId);
  if (!area || area.parentId !== "nodes") return `${pageHeader("Nodes", "Node group not found", areaId)}${emptyState("Unknown node group", "Choose one of the registered node categories.")}`;
  const modules = Object.entries(area.counts.modules).sort((a, b) => a[0].localeCompare(b[0]));
  return `${pageHeader("Node group", area.label, area.description, `<a class="button primary" href="#relationships/${encodeURIComponent(`area:${area.id}`)}">Open graph</a>`)}
    <div class="category-tabs">${categories.map((category) => `<a href="#nodes/${encodeURIComponent(category.id)}" class="${category.id === area.id ? "active" : ""}">${escapeHtml(category.label)}<span>${category.counts.units}</span></a>`).join("")}</div>
    ${modules.map(([moduleName]) => {
      const units = area.unitIds.map((id) => registryState.unitsById.get(id)).filter((unit) => unit?.module === moduleName);
      return `<section class="panel module-group"><div class="panel-heading"><div><span class="eyebrow">Node module</span><h2>${escapeHtml(moduleName)}</h2></div><span>${units.length} units</span></div><div class="unit-list">${units.map((unit) => unitRow(unit, { showModule: false })).join("")}</div></section>`;
    }).join("")}`;
}

function relationshipsView(nodeId) {
  let context = { type: "area", id: "workspaces", label: "Workspaces" };
  if (nodeId) {
    const node = registryState.graphNodesById.get(nodeId);
    if (node) context = { type: "node", id: node.id, label: node.label };
    else if (registryState.unitsById.has(nodeId)) context = { type: "unit", id: nodeId, label: registryState.unitsById.get(nodeId).label };
    else if (registryState.modulesById.has(nodeId)) context = { type: "module", id: nodeId, label: registryState.modulesById.get(nodeId).path };
  }
  return {
    html: `${pageHeader("Contextual graph", "Relationships", "Explore only the selected architecture neighborhood. Increase depth deliberately instead of rendering the entire repository at once.")}
      <section class="graph-panel">${graphToolbar(context.label)}<div class="graph-stage"><div id="graph-canvas" aria-label="Interactive source relationship graph"></div><div class="graph-legend"><span><i class="area"></i>Area</span><span><i class="module"></i>Module</span><span><i class="unit"></i>Logical unit</span><span><i class="file"></i>File</span><span class="graph-status" id="graph-status"></span></div></div></section>`,
    graphContext: context,
  };
}

function reviewQueueView() {
  const { data } = registryState;
  const pending = data.files.filter((file) => file.reviewStatus !== "reviewed").sort((left, right) => String(left.lastModifiedAt).localeCompare(String(right.lastModifiedAt)));
  const issueEntries = Object.entries(data.audit.issues || {}).filter(([, values]) => values.length);
  return `${pageHeader("Semantic integrity", "Review Queue", "Hash changes and validation failures remain visible until a human or agent confirms the file's current architectural role.")}
    <section class="audit-banner ${pending.length || issueEntries.length ? "failed" : "approved"}"><span class="audit-icon">${pending.length ? "!" : "✓"}</span><div><strong>${pending.length ? `${pending.length} files need semantic review` : "No pending semantic reviews"}</strong><p>${issueEntries.length ? `${issueEntries.length} validation categories report issues.` : "All registered hashes match their last reviewed version."}</p></div></section>
    ${pending.length ? `<section class="panel"><div class="panel-heading"><div><span class="eyebrow">Changed source</span><h2>Pending files</h2></div></div><div class="file-list">${pending.map(fileRow).join("")}</div></section>` : emptyState("Queue is clear", "The current registry has no changed files awaiting semantic confirmation.")}
    ${issueEntries.length ? `<section class="panel issue-list"><div class="panel-heading"><div><span class="eyebrow">Validation</span><h2>Reported issues</h2></div></div>${issueEntries.map(([category, values]) => `<details><summary>${escapeHtml(category)} <span>${values.length}</span></summary><ul>${values.map((value) => `<li>${escapeHtml(value)}</li>`).join("")}</ul></details>`).join("")}</section>` : ""}`;
}

function uncategorizedView() {
  const files = registryState.data.files.filter((file) => file.category === "uncategorized" || !file.productAreaIds?.length);
  return `${pageHeader("Classification queue", "Uncategorized", "New files that do not match a Code Registry category or Product Area remain visible here until their ownership is defined.")}
    ${files.length ? `<section class="audit-banner failed"><span class="audit-icon">!</span><div><strong>${files.length} files need classification</strong><p>Reload data discovers new files and removes entries for deleted files automatically.</p></div></section><section class="panel"><div class="panel-heading"><div><span class="eyebrow">Unclassified source</span><h2>Files awaiting categorization</h2></div></div><div class="file-list">${files.map(fileRow).join("")}</div></section>` : emptyState("No uncategorized files", "Every registered file currently belongs to a category and Product Area.")}`;
}

function allFilesView(container) {
  const { data } = registryState;
  const controls = `<div class="filter-bar">
    <label>Filter<input id="file-query" type="search" placeholder="Path, name or description"></label>
    <label>Language<select id="file-language"><option value="">All languages</option>${data.filters.languages.map((value) => `<option>${escapeHtml(value)}</option>`).join("")}</select></label>
    <label>Role<select id="file-role"><option value="">All roles</option>${data.roles.map((role) => `<option value="${escapeHtml(role.id)}">${escapeHtml(role.label)}</option>`).join("")}</select></label>
    <label>Review<select id="file-review"><option value="">Any review status</option><option value="reviewed">Reviewed</option><option value="needs_review">Needs review</option></select></label>
    <label>Sort<select id="file-sort"><option value="modified-newest">Recently modified</option><option value="modified-oldest">Oldest modified</option><option value="path">Path</option></select></label>
  </div>`;
  container.innerHTML = `${pageHeader("Complete inventory", "All Files", "Filter individual maintained files without opening the 49,000-line modular registry.")}${controls}<p class="result-count" id="file-result-count"></p><div class="file-list" id="filtered-files"></div>`;
  const renderResults = () => {
    const query = container.querySelector("#file-query").value.toLocaleLowerCase().trim();
    const language = container.querySelector("#file-language").value;
    const role = container.querySelector("#file-role").value;
    const review = container.querySelector("#file-review").value;
    const sort = container.querySelector("#file-sort").value;
    const matches = data.files.filter((file) => {
      const text = `${file.path} ${file.description} ${file.module}`.toLocaleLowerCase();
      return (!query || text.includes(query)) && (!language || file.language === language) && (!role || file.architecturalRole === role) && (!review || file.reviewStatus === review);
    });
    matches.sort((left, right) => sort === "modified-oldest"
      ? String(left.lastModifiedAt).localeCompare(String(right.lastModifiedAt))
      : sort === "modified-newest"
        ? String(right.lastModifiedAt).localeCompare(String(left.lastModifiedAt))
        : left.path.localeCompare(right.path));
    container.querySelector("#file-result-count").textContent = `${matches.length} matching files${matches.length > 150 ? " · showing first 150" : ""}`;
    container.querySelector("#filtered-files").innerHTML = matches.length ? matches.slice(0, 150).map(fileRow).join("") : emptyState("No files match", "Change one or more filters to broaden the inventory.");
    return container.querySelector("#filtered-files");
  };
  const resultContainer = renderResults();
  ["#file-query", "#file-language", "#file-role", "#file-review", "#file-sort"].forEach((selector) => container.querySelector(selector).addEventListener("input", () => {
    const updated = renderResults();
    container.dispatchEvent(new CustomEvent("registry:rebind", { detail: updated }));
  }));
  return resultContainer;
}

function codeTree(files, selectedPath) {
  const root = { folders: new Map(), files: [] };
  files.forEach((file) => {
    const parts = file.path.split("/");
    const filename = parts.pop();
    let current = root;
    parts.forEach((part) => {
      if (!current.folders.has(part)) current.folders.set(part, { folders: new Map(), files: [] });
      current = current.folders.get(part);
    });
    current.files.push({ ...file, filename });
  });
  const renderBranch = (branch, prefix = "") => {
    const folders = [...branch.folders.entries()].sort(([left], [right]) => left.localeCompare(right)).map(([name, child]) => {
      const folderPath = prefix ? `${prefix}/${name}` : name;
      const open = selectedPath === folderPath || selectedPath.startsWith(`${folderPath}/`) ? " open" : "";
      return `<details class="code-tree-folder"${open}><summary>${escapeHtml(name)}</summary><div>${renderBranch(child, folderPath)}</div></details>`;
    });
    const leaves = branch.files.sort((left, right) => left.filename.localeCompare(right.filename)).map((file) => `<a class="code-tree-file ${file.path === selectedPath ? "active" : ""}" href="#code-reader/${encodeURIComponent(file.path)}"><span>${escapeHtml(file.extension || "file")}</span>${escapeHtml(file.filename)}</a>`);
    return `${folders.join("")}${leaves.join("")}`;
  };
  return renderBranch(root);
}

function codeReaderView(path) {
  const files = [...registryState.data.files].sort((left, right) => left.path.localeCompare(right.path));
  const selected = registryState.filesByPath.has(path) ? path : files[0]?.path;
  if (!selected) return `${pageHeader("Local source", "Code Reader", "There are no registered source files to display.")}${emptyState("No source files", "Run Reload data after adding a maintained file.")}`;
  return `${pageHeader("Local source", "Code Reader", "Read only registered code through the localhost Explorer. The reader never exposes paths outside the Registry inventory.")}
    <section class="code-reader panel"><aside class="code-tree"><div class="code-tree-heading"><span>Explorer</span><small>${files.length} files</small></div><nav>${codeTree(files, selected)}</nav></aside><div class="code-reader-main"><div class="code-reader-toolbar"><code>${escapeHtml(selected)}</code><div class="code-reader-version"><select id="code-reader-version" aria-label="Source version"><option value="working">Working tree</option><option value="HEAD">HEAD</option></select><button class="button secondary" id="code-reader-diff" type="button">View Git diff</button><span id="code-reader-meta">Loading source…</span></div></div><div class="code-reader-body" id="code-reader-content"></div></div></section>`;
}

function globalCodeSearchView() {
  return `${pageHeader("Literal source search", "Global Search", "Find an exact word or phrase across every registered source file. Results show matching files, occurrences and source lines.")}
    <section class="panel source-search-panel">
      <label class="source-search-input">Search source text<input id="source-search-query" type="search" placeholder="Example: licenseActivateWithKey or timelineBackground" autocomplete="off"></label>
      <p>Search is literal and case-insensitive. It returns up to 100 files and five line excerpts per file.</p>
    </section>
    <p class="result-count" id="source-search-count">Enter a word or phrase to search all registered code.</p>
    <div class="source-search-results" id="source-search-results"></div>`;
}

function gitAuditView() {
  return `${pageHeader("Repository integrity", "Git Audit", "Compare the working tree, registered code, commits and GitHub remote without creating commits, pulls, pushes or conflict resolutions.")}
    <section class="git-audit-actions"><button class="button secondary" id="git-audit-refresh" type="button">Refresh local state</button><button class="button primary" id="git-audit-fetch" type="button">Check Git remote</button></section>
    <div id="git-audit-content" class="git-audit-content"><div class="empty-state"><span>◆</span><h3>Loading Git audit</h3><p>Reading local repository state.</p></div></div>`;
}

export function renderView(container, route, onInspect) {
  let result = { html: "", graphContext: null };
  if (route.view === "overview") result.html = overviewView();
  else if (route.view === "architecture") result.html = architectureView(route.id);
  else if (route.view === "product-areas") result.html = productAreasView(route.id);
  else if (route.view === "nodes") result.html = nodesView(route.id);
  else if (route.view === "relationships") result = relationshipsView(route.id);
  else if (route.view === "review-queue") result.html = reviewQueueView();
  else if (route.view === "uncategorized") result.html = uncategorizedView();
  else if (route.view === "code-reader") result.html = codeReaderView(route.id);
  else if (route.view === "global-code-search") result.html = globalCodeSearchView();
  else if (route.view === "git-audit") result.html = gitAuditView();
  else if (route.view === "all-files") {
    const list = allFilesView(container);
    bindInspectTriggers(list, onInspect);
    return { graphContext: null };
  } else result.html = overviewView();
  container.innerHTML = result.html;
  bindInspectTriggers(container, onInspect);
  return result;
}
