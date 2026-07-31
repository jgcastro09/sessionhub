import { registryState, roleInfo } from "./state.js";

const NODE_TYPE_PRIORITY = { area: 0, module: 1, unit: 2, file: 3 };

function visualColor(node) {
  if (node.color) return node.color;
  if (node.type === "module") return "#475569";
  if (node.type === "file") return "#64748b";
  return roleInfo(node.role).color;
}

function hierarchySeeds(type, id) {
  if (type === "area") {
    const area = registryState.areasById.get(id);
    if (!area) return [];
    const children = registryState.data.productAreas.filter((item) => item.parentId === id).map((item) => `area:${item.id}`);
    return [`area:${id}`, ...children, ...area.directUnitIds];
  }
  if (type === "module") {
    const module = registryState.modulesById.get(id) || registryState.modulesByPath.get(id);
    if (!module) return [];
    const children = registryState.data.modules
      .filter((item) => item.path.startsWith(`${module.path}/`) && item.path.split("/").length === module.path.split("/").length + 1)
      .map((item) => item.id);
    return [module.id, ...children, ...module.directUnitIds];
  }
  if (type === "node") return registryState.graphNodesById.has(id) ? [id] : [];
  if (type === "unit") return registryState.unitsById.has(id) ? [id] : [];
  if (type === "file") return registryState.filesByPath.has(id) ? [`file:${id}`] : [];
  return [];
}

function nodeRank(node) {
  return [
    node.reviewStatus === "needs_review" ? 0 : 1,
    node.criticality === "critical" ? 0 : node.criticality === "important" ? 1 : 2,
    NODE_TYPE_PRIORITY[node.type] ?? 9,
    String(node.label).toLocaleLowerCase(),
  ];
}

function compareRank(left, right) {
  const a = nodeRank(left);
  const b = nodeRank(right);
  for (let index = 0; index < a.length; index += 1) {
    if (a[index] < b[index]) return -1;
    if (a[index] > b[index]) return 1;
  }
  return 0;
}

function collectSubgraph(seedIds, depth, limit) {
  const selected = new Set(seedIds.filter((id) => registryState.graphNodesById.has(id)));
  let frontier = [...selected];
  for (let level = 0; level < depth && selected.size < limit; level += 1) {
    const candidates = new Set();
    frontier.forEach((id) => {
      (registryState.adjacency.get(id) || []).forEach((edge) => {
        const other = edge.source === id ? edge.target : edge.source;
        if (!selected.has(other)) candidates.add(other);
      });
    });
    const ordered = [...candidates]
      .map((id) => registryState.graphNodesById.get(id))
      .filter(Boolean)
      .sort(compareRank)
      .slice(0, Math.max(0, limit - selected.size));
    frontier = ordered.map((node) => node.id);
    frontier.forEach((id) => selected.add(id));
    if (!frontier.length) break;
  }
  const edges = registryState.data.graph.edges.filter((edge) => selected.has(edge.source) && selected.has(edge.target));
  return { nodeIds: [...selected], edges, truncated: selected.size >= limit };
}

function entityFromGraphNode(node) {
  if (node.type === "file") return ["file", node.path];
  if (node.type === "unit") return ["unit", node.id];
  if (node.type === "module") return ["module", node.id];
  if (node.type === "area") return ["area", node.path];
  return ["node", node.id];
}

export class GraphView {
  constructor(container, onSelect, onNotice = () => {}) {
    this.container = container;
    this.onSelect = onSelect;
    this.onNotice = onNotice;
    this.context = { type: "area", id: "workspaces", label: "Workspaces" };
    this.depth = registryState.data.graphSettings.defaultDepth;
    this.limit = registryState.data.graphSettings.initialNodeLimit;
    this.layout = "breadthfirst";
    this.selectedId = null;
    this.cy = null;
  }

  setContext(type, id, label = "") {
    this.context = { type, id, label };
    this.render();
  }

  setOptions({ depth, limit, layout }) {
    if (depth) this.depth = Number(depth);
    if (limit) this.limit = Math.min(Number(limit), registryState.data.graphSettings.maximumNodeLimit);
    if (layout) this.layout = layout;
    this.render();
  }

  render() {
    if (!window.cytoscape) {
      this.container.innerHTML = '<div class="graph-error">Cytoscape.js could not be loaded.</div>';
      return;
    }
    const seedIds = hierarchySeeds(this.context.type, this.context.id);
    if (!seedIds.length) {
      this.container.innerHTML = '<div class="graph-error">No graph context is available for this selection.</div>';
      return;
    }
    const subgraph = collectSubgraph(seedIds, this.depth, this.limit);
    const elements = [
      ...subgraph.nodeIds.map((id) => {
        const node = registryState.graphNodesById.get(id);
        return {
          group: "nodes",
          data: {
            ...node,
            color: visualColor(node),
            shortLabel: String(node.label).length > 30 ? `${String(node.label).slice(0, 28)}…` : node.label,
          },
          classes: `${node.type} ${node.role} ${node.reviewStatus} ${node.criticality}`,
        };
      }),
      ...subgraph.edges.map((edge) => ({
        group: "edges",
        data: { ...edge, label: edge.type.replaceAll("-", " ") },
        classes: `${edge.type} ${edge.confidence}`,
      })),
    ];
    if (this.cy) this.cy.destroy();
    this.cy = window.cytoscape({
      container: this.container,
      elements,
      minZoom: 0.18,
      maxZoom: 2.8,
      wheelSensitivity: 0.18,
      selectionType: "single",
      boxSelectionEnabled: true,
      style: [
        { selector: "node", style: {
          "background-color": "data(color)", "border-width": 2, "border-color": "#0f172a",
          label: "data(shortLabel)", color: "#e2e8f0", "font-size": 10, "font-family": "Segoe UI, sans-serif",
          "text-valign": "bottom", "text-margin-y": 7, "text-wrap": "ellipsis", "text-max-width": 130,
          width: 28, height: 28, "overlay-opacity": 0,
        } },
        { selector: "node.area", style: { shape: "round-rectangle", width: 92, height: 42, "font-size": 12, "font-weight": 700 } },
        { selector: "node.module", style: { shape: "round-rectangle", width: 72, height: 34 } },
        { selector: "node.unit", style: { shape: "round-diamond", width: 34, height: 34 } },
        { selector: "node.file", style: { shape: "rectangle", width: 23, height: 29, "font-size": 9, "min-zoomed-font-size": 11 } },
        { selector: "node.needs_review", style: { "border-color": "#facc15", "border-width": 4 } },
        { selector: "node.critical", style: { "border-color": "#fb7185", "border-width": 4 } },
        { selector: "node:selected", style: { "border-color": "#ffffff", "border-width": 4, "shadow-blur": 18, "shadow-color": "#60a5fa", "shadow-opacity": 0.7 } },
        { selector: "edge", style: {
          width: 1.2, "line-color": "#334155", "target-arrow-color": "#334155", "target-arrow-shape": "triangle",
          "curve-style": "bezier", opacity: 0.58, "arrow-scale": 0.65, "overlay-opacity": 0,
        } },
        { selector: "edge.contains", style: { "line-color": "#475569", "target-arrow-shape": "none", width: 1.8 } },
        { selector: "edge.implements", style: { "line-color": "#38bdf8", "target-arrow-color": "#38bdf8" } },
        { selector: "edge.includes", style: { "line-color": "#64748b", "target-arrow-color": "#64748b" } },
        { selector: "edge.imports", style: { "line-color": "#22c55e", "target-arrow-color": "#22c55e" } },
        { selector: "edge.related", style: { "line-color": "#a78bfa", "target-arrow-color": "#a78bfa", "line-style": "dashed" } },
        { selector: "edge.probable", style: { "line-color": "#64748b", "line-style": "dotted", opacity: 0.35 } },
        { selector: "edge:selected", style: { width: 3, opacity: 1, label: "data(label)", color: "#f8fafc", "font-size": 10, "text-background-color": "#0f172a", "text-background-opacity": 0.9, "text-background-padding": 3 } },
      ],
    });
    this.runLayout();
    this.cy.on("tap", "node", (event) => {
      const node = registryState.graphNodesById.get(event.target.id());
      if (!node) return;
      this.selectedId = node.id;
      const [type, id] = entityFromGraphNode(node);
      this.onSelect(type, id);
    });
    this.cy.on("tap", "edge", (event) => {
      const edge = event.target.data();
      this.onNotice(`${edge.type}: ${registryState.graphNodesById.get(edge.source)?.label} → ${registryState.graphNodesById.get(edge.target)?.label}`);
    });
    const status = document.getElementById("graph-status");
    if (status) {
      status.textContent = `${subgraph.nodeIds.length} nodes · ${subgraph.edges.length} relations${subgraph.truncated ? " · limit reached" : ""}`;
      status.classList.toggle("warning", subgraph.truncated);
    }
  }

  runLayout() {
    if (!this.cy) return;
    const options = this.layout === "breadthfirst"
      ? { name: "breadthfirst", directed: true, spacingFactor: 1.35, avoidOverlap: true, padding: 42, animate: false }
      : this.layout === "cose"
        ? { name: "cose", idealEdgeLength: 90, nodeRepulsion: 5400, gravity: 0.32, padding: 42, animate: false, randomize: true }
        : { name: this.layout, padding: 42, animate: false };
    this.cy.layout(options).run();
  }

  fit() {
    this.cy?.fit(undefined, 42);
  }

  focusSelection() {
    if (!this.selectedId) {
      this.onNotice("Select a graph node before focusing.");
      return;
    }
    this.context = { type: "node", id: this.selectedId, label: registryState.graphNodesById.get(this.selectedId)?.label || "Selection" };
    this.render();
  }

  destroy() {
    this.cy?.destroy();
    this.cy = null;
  }
}
