// These aliases are exercised by Node reload tests and the real Vite browser probe.
declare module "*api.ts?retry-first" {
  const api: typeof import("../lib/api");
  export = api;
}
declare module "*api.ts?retry-reload" {
  const api: typeof import("../lib/api");
  export = api;
}
declare module "*api.ts?retry-storage-denied" {
  const api: typeof import("../lib/api");
  export = api;
}
declare module "*src/connectors/templates/registry.jsx" {
  const registry: typeof import("../connectors/templates/registry");
  export = registry;
}
declare module "*src/connectors/templates/redis/model.ts" {
  const model: typeof import("../connectors/templates/redis/model");
  export = model;
}
declare module "*src/connectors/templates/clickhouse/model.ts" {
  const model: typeof import("../connectors/templates/clickhouse/model");
  export = model;
}
declare module "*src/connectors/templates/rabbitmq/model.ts" {
  const model: typeof import("../connectors/templates/rabbitmq/model");
  export = model;
}
