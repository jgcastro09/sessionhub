import { registryState, roleInfo, routeFor } from "./state.js";

export function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

export function formatDate(value) {
  if (!value) return "Not generated";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium", timeStyle: "short",
  }).format(date);
}

export function statusPill(status) {
  const label = status === "reviewed" ? "Reviewed" : status === "needs_review" ? "Needs review" : status;
  return `<span class="status-pill ${escapeHtml(status)}">${escapeHtml(label)}</span>`;
}

export function rolePill(roleId) {
  const role = roleInfo(roleId);
  return `<span class="role-pill"><i style="--role-color:${escapeHtml(role.color)}"></i>${escapeHtml(role.label)}</span>`;
}

export function metricCard(label, value, detail = "", tone = "") {
  return `<article class="metric-card ${escapeHtml(tone)}">
    <span>${escapeHtml(label)}</span><strong>${escapeHtml(value)}</strong>${detail ? `<small>${escapeHtml(detail)}</small>` : ""}
  </article>`;
}

export function emptyState(title, detail) {
  return `<div class="empty-state"><span>◇</span><h3>${escapeHtml(title)}</h3><p>${escapeHtml(detail)}</p></div>`;
}

export function unitRow(unit, options = {}) {
  const { showModule = true } = options;
  return `<button class="unit-row inspect-trigger" data-entity-type="unit" data-entity-id="${escapeHtml(unit.id)}" type="button">
    <span class="unit-icon ${escapeHtml(unit.role)}">${unit.filePaths.length > 1 ? "◫" : "□"}</span>
    <span class="unit-main"><strong>${escapeHtml(unit.label)}</strong><small>${escapeHtml(unit.description)}</small></span>
    ${showModule ? `<span class="unit-module">${escapeHtml(unit.module)}</span>` : ""}
    ${rolePill(unit.role)}
    ${statusPill(unit.reviewStatus)}
    <span class="unit-count">${unit.filePaths.length} file${unit.filePaths.length === 1 ? "" : "s"}</span>
  </button>`;
}

export function areaCard(area) {
  return `<a class="area-card" href="${routeFor("area", area.id)}" style="--area-color:${escapeHtml(area.color)}">
    <div class="area-card-top"><span class="area-symbol">◇</span><span>${area.counts.pending ? statusPill("needs_review") : ""}</span></div>
    <h3>${escapeHtml(area.label)}</h3>
    <p>${escapeHtml(area.description)}</p>
    <footer><strong>${area.counts.units}</strong> units <span>·</span> <strong>${area.counts.files}</strong> files</footer>
  </a>`;
}

export function fileRow(file) {
  return `<button class="file-row inspect-trigger" data-entity-type="file" data-entity-id="${escapeHtml(file.path)}" type="button">
    <span class="file-kind">${escapeHtml(file.extension || file.kind)}</span>
    <span class="file-main"><strong>${escapeHtml(file.filename)}</strong><small>${escapeHtml(file.path)}</small></span>
    <span>${escapeHtml(file.module)}</span>
    <span>${escapeHtml(file.language)}</span>
    <span class="file-modified">${escapeHtml(formatDate(file.lastModifiedAt))}</span>
    ${rolePill(file.architecturalRole)}
    ${statusPill(file.reviewStatus)}
  </button>`;
}

export function architectureTree(nodes, depth = 0) {
  return nodes.map((node) => {
    const children = node.children?.length ? architectureTree(node.children, depth + 1) : "";
    const href = `#architecture/${encodeURIComponent(node.path)}`;
    return `<li>
      <div class="tree-row" style="--depth:${depth}">
        <span class="tree-branch">${children ? "▾" : "·"}</span>
        <a href="${href}">${escapeHtml(node.label)}</a>
        <span>${node.unitCount}</span>
      </div>
      ${children ? `<ul>${children}</ul>` : ""}
    </li>`;
  }).join("");
}

export function barList(counts, total, maxItems = 12) {
  return Object.entries(counts)
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .slice(0, maxItems)
    .map(([label, value]) => `<div class="bar-row">
      <span>${escapeHtml(label)}</span><div><i style="width:${Math.max(2, (value / Math.max(total, 1)) * 100)}%"></i></div><strong>${value}</strong>
    </div>`).join("");
}

export function bindInspectTriggers(container, handler) {
  container.querySelectorAll(".inspect-trigger").forEach((element) => {
    element.addEventListener("click", () => handler(element.dataset.entityType, element.dataset.entityId));
  });
}

export function graphToolbar(contextLabel = "Architecture graph") {
  const settings = registryState.data.graphSettings;
  return `<div class="graph-toolbar">
    <div><span class="eyebrow">Graph context</span><strong id="graph-context-label">${escapeHtml(contextLabel)}</strong></div>
    <label>Layout<select id="graph-layout"><option value="breadthfirst">Hierarchy</option><option value="cose">Relations</option><option value="concentric">Concentric</option><option value="circle">Circle</option></select></label>
    <label>Depth<select id="graph-depth">${[1, 2, 3].map((value) => `<option value="${value}" ${value === settings.defaultDepth ? "selected" : ""}>${value}</option>`).join("")}</select></label>
    <label>Limit<select id="graph-limit">${[settings.initialNodeLimit, 120, settings.maximumNodeLimit].filter((value, index, values) => values.indexOf(value) === index).map((value) => `<option value="${value}">${value}</option>`).join("")}</select></label>
    <button class="button secondary" id="graph-fit" type="button">Fit</button>
    <button class="button secondary" id="graph-focus" type="button">Focus selection</button>
  </div>`;
}
