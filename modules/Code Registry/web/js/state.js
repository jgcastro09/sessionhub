export const registryState = {
  data: null,
  filesByPath: new Map(),
  unitsById: new Map(),
  modulesById: new Map(),
  modulesByPath: new Map(),
  areasById: new Map(),
  graphNodesById: new Map(),
  graphEdgesById: new Map(),
  adjacency: new Map(),
  roleById: new Map(),
  searchRecords: [],
};

function flattenedText(values) {
  return values.flat(Infinity).filter(Boolean).join(" ").toLocaleLowerCase();
}

function addAdjacent(id, edge) {
  if (!registryState.adjacency.has(id)) registryState.adjacency.set(id, []);
  registryState.adjacency.get(id).push(edge);
}

export function installRegistryData(data) {
  registryState.data = data;
  registryState.filesByPath = new Map(data.files.map((file) => [file.path, file]));
  registryState.unitsById = new Map(data.logicalUnits.map((unit) => [unit.id, unit]));
  registryState.modulesById = new Map(data.modules.map((module) => [module.id, module]));
  registryState.modulesByPath = new Map(data.modules.map((module) => [module.path, module]));
  registryState.areasById = new Map(data.productAreas.map((area) => [area.id, area]));
  registryState.graphNodesById = new Map(data.graph.nodes.map((node) => [node.id, node]));
  registryState.graphEdgesById = new Map(data.graph.edges.map((edge) => [edge.id, edge]));
  registryState.roleById = new Map(data.roles.map((role) => [role.id, role]));
  registryState.adjacency = new Map();
  data.graph.edges.forEach((edge) => {
    addAdjacent(edge.source, edge);
    addAdjacent(edge.target, edge);
  });

  const records = [];
  data.files.forEach((file) => records.push({
    type: "file",
    id: file.path,
    label: file.filename,
    subtitle: `${file.description} · Modified ${file.lastModifiedAt || "unknown"}`,
    path: file.path,
    search: flattenedText([
      file.path, file.filename, file.module, file.description, file.language, file.lastModifiedAt,
      file.responsibilities, file.productAreaIds,
      Object.values(file.symbols || {}),
    ]),
  }));
  data.logicalUnits.forEach((unit) => records.push({
    type: "unit", id: unit.id, label: unit.label, subtitle: unit.description,
    path: unit.primaryPath,
    search: flattenedText([unit.label, unit.module, unit.description, unit.filePaths, unit.role]),
  }));
  data.modules.forEach((module) => records.push({
    type: "module", id: module.id, label: module.path, subtitle: `${module.unitCount} logical units`,
    path: module.path,
    search: flattenedText([module.path, module.label]),
  }));
  data.productAreas.forEach((area) => records.push({
    type: "area", id: area.id, label: area.label, subtitle: area.description,
    path: area.id,
    search: flattenedText([area.id, area.label, area.description, Object.keys(area.counts.modules)]),
  }));
  registryState.searchRecords = records;
}

export function roleInfo(roleId) {
  return registryState.roleById.get(roleId) || { id: roleId, label: roleId, color: "#94a3b8" };
}

export function searchRegistry(query, limit = 60) {
  const terms = query.toLocaleLowerCase().trim().split(/\s+/).filter(Boolean);
  if (!terms.length) return [];
  return registryState.searchRecords
    .map((record) => ({ record, score: terms.reduce((score, term) => {
      if (!record.search.includes(term)) return -1000;
      if (record.label.toLocaleLowerCase() === term) return score + 20;
      if (record.label.toLocaleLowerCase().startsWith(term)) return score + 10;
      if (record.path.toLocaleLowerCase().includes(term)) return score + 5;
      return score + 1;
    }, 0) }))
    .filter(({ score }) => score >= 0)
    .sort((a, b) => b.score - a.score || a.record.label.localeCompare(b.record.label))
    .slice(0, limit)
    .map(({ record }) => record);
}

export function formatBytes(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function routeFor(type, id) {
  if (type === "area") return `#product-areas/${encodeURIComponent(id)}`;
  if (type === "module") {
    const module = registryState.modulesById.get(id);
    return `#architecture/${encodeURIComponent(module?.path || id)}`;
  }
  if (type === "unit") return `#relationships/${encodeURIComponent(id)}`;
  if (type === "file") return `#relationships/${encodeURIComponent(`file:${id}`)}`;
  return "#overview";
}
