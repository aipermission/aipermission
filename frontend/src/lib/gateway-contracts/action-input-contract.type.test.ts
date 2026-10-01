import type { components } from "../../../types/generated-openapi";

type ActionDefinition = components["schemas"]["ConnectorActionDefinition"];
const action: ActionDefinition = {
  name: "inspect",
  label: "Inspect",
  description: "Inspect state",
  risk: "read",
  input_schema: { fields: [] },
  retry_policy: { class: "read_only", guidance: "Read again." },
  max_input_bytes: 1024,
};
const limit: number = action.max_input_bytes;
// @ts-expect-error Published input limits are numeric, not strings.
const invalid: ActionDefinition = { ...action, max_input_bytes: "1024" };
const { max_input_bytes: omitted, ...withoutLimit } = action;
// @ts-expect-error The generated wire contract requires an input limit.
const missing: ActionDefinition = withoutLimit;
void limit;
void invalid;
void omitted;
void missing;
