import { z } from "zod";
import { connectorActionStatuses, connectorRetryClasses } from "./generated-connector-contract.js";

const positiveID = z.number().int().positive();
const nonNegativeInteger = z.number().int().nonnegative();
const forbiddenMetadataField = /(?:^|_)(?:credential|password|passphrase|private_key|secret|token|api_key|access_key)(?:$|_)/;

function normalizeMetadataKey(key) {
  return key
    .trim()
    .replace(/([a-z0-9])([A-Z])/g, "$1_$2")
    .replace(/[^A-Za-z0-9]+/g, "_")
    .toLowerCase();
}

function rejectSecretMetadataKeys(value, context) {
  const pending = [value];
  while (pending.length > 0) {
    const current = pending.pop();
    if (Array.isArray(current)) {
      pending.push(...current);
      continue;
    }
    if (!current || typeof current !== "object") continue;
    for (const [key, item] of Object.entries(current)) {
      const normalized = normalizeMetadataKey(key);
      if (forbiddenMetadataField.test(normalized)) {
        context.addIssue({ code: z.ZodIssueCode.custom, message: "connector metadata contains a forbidden secret field" });
        return;
      }
      pending.push(item);
    }
  }
}

const connectorMetadataSchema = z.record(z.unknown()).superRefine(rejectSecretMetadataKeys);

const actionGrantSchema = z
  .object({
    name: z.string(),
    execution_rule: z.string(),
    expires_at: z.string().optional(),
  })
  .strict();

const connectorTargetSchema = z
  .object({
    target_ref: z.string(),
    project_id: positiveID,
    project_name: z.string(),
    project_slug: z.string(),
    target_id: positiveID,
    target_name: z.string(),
    connector_kind: z.string(),
    profile_id: positiveID,
    profile_label: z.string(),
    profile_kind: z.string(),
    metadata: connectorMetadataSchema.optional(),
    actions: z.array(actionGrantSchema),
    hints: z.array(z.string()).optional(),
  })
  .strict();

const connectorHelpSchema = z
  .object({
    title: z.string(),
    summary: z.string(),
    usage: z.array(z.string()).optional(),
    warnings: z.array(z.string()).optional(),
    connector: z.string(),
    connector_id: z.string(),
  })
  .strict();

const fieldOptionSchema = z.object({ value: z.string(), label: z.string() }).strict();
const fieldSchema = z
  .object({
    name: z.string(),
    label: z.string(),
    type: z.string(),
    required: z.boolean().optional(),
    preserve_whitespace: z.boolean().optional(),
    secret: z.boolean().optional(),
    description: z.string().optional(),
    default: z.unknown().optional(),
    options: z.array(fieldOptionSchema).optional(),
  })
  .strict();
const inputSchema = z.object({ fields: z.array(fieldSchema) }).strict();
const outputHintSchema = z
  .object({
    format: z.string().optional(),
    sensitive_fields: z.array(z.string()).optional(),
    temporary_capability_fields: z.array(z.string()).optional(),
    max_rows: nonNegativeInteger.optional(),
    max_bytes: nonNegativeInteger.optional(),
  })
  .strict();
const retryPolicySchema = z
  .object({
    class: z.enum([...connectorRetryClasses]),
    precondition_fields: z.array(z.string()).optional(),
    guidance: z.string(),
  })
  .strict();
const actionDefinitionSchema = z
  .object({
    name: z.string(),
    label: z.string(),
    description: z.string(),
    category: z.string().optional(),
    risk: z.string(),
    input_schema: inputSchema,
    sensitive_input_fields: z.array(z.string()).optional(),
    output_hint: outputHintSchema.optional(),
    retry_policy: retryPolicySchema,
    max_input_bytes: positiveID,
  })
  .strict();

const connectorActionsSchema = z.object({ items: z.array(actionDefinitionSchema) }).strict();

const connectorActionRequestSchema = z
  .object({
    status: z.enum([...connectorActionStatuses]),
    request_id: positiveID,
    target_ref: z.string(),
    target_name: z.string().optional(),
    connector_kind: z.string(),
    profile_label: z.string().optional(),
    action_name: z.string(),
    input: z.record(z.unknown()).optional(),
    // Connector-owned output is intentionally opaque. The gateway credential
    // boundary redacts values; this schema owns only the shared MCP envelope.
    output: z.unknown().optional(),
    display_text: z.string().optional(),
    error: z.string().optional(),
    retry_policy: retryPolicySchema,
    retry_after_seconds: nonNegativeInteger.max(3600).optional(),
    assistant_hint: z.string().optional(),
    output_withheld: z.boolean().optional(),
    replayed: z.boolean().optional(),
  })
  .strict()
  .superRefine((value, context) => {
    if (value.output_withheld) {
      if (value.input !== undefined || value.output !== undefined || value.display_text !== undefined || value.error !== undefined) {
        context.addIssue({ code: z.ZodIssueCode.custom, message: "withheld action output must not contain request or result content" });
      }
    } else if (!value.target_ref || !value.connector_kind || !value.action_name) {
      context.addIssue({ code: z.ZodIssueCode.custom, message: "action response identity is required" });
    }
  });
const connectorActionStoppedSchema = z.object({ status: z.literal("stopped"), error: z.string().min(1) }).strict();
const connectorActionCallSchema = z.union([connectorActionRequestSchema, connectorActionStoppedSchema]);

const vaultItemSchema = z
  .object({
    vault_ref: z.string(),
    item_id: positiveID,
    project_ref: z.string(),
    source_project_id: positiveID,
    name: z.string(),
    secret_type: z.string(),
    status: z.string(),
    expires_at: z.string().optional(),
    value_version: positiveID,
    metadata_revision: positiveID,
  })
  .strict();
const vaultItemsSchema = z
  .object({
    items: z.array(vaultItemSchema),
    count: nonNegativeInteger,
    truncated: z.boolean(),
    secret_values_returned: z.literal(false),
  })
  .strict();

const usageNoteSchema = z.object({ location: z.string(), notes: z.string().optional() }).strict();
const vaultGenerateInputSchema = z
  .object({
    name: z.string(),
    secret_type: z.string().optional(),
    generator_kind: z.string(),
    provider: z.string().optional(),
    environment: z.string().optional(),
    description: z.string().optional(),
    expires_at: z.string().optional(),
    expiry_warning_days: nonNegativeInteger.optional(),
    tags: z.array(z.string()).optional(),
    usage_notes: z.array(usageNoteSchema).optional(),
    shared_project_ids: z.array(positiveID).optional(),
  })
  .strict();
const vaultSessionItemSchema = z
  .object({
    item_id: positiveID,
    source_project_id: positiveID,
    replace_existing: z.boolean().optional(),
  })
  .strict();
const vaultSessionInputSchema = z
  .object({
    target_ref: z.string(),
    items: z.array(vaultSessionItemSchema),
  })
  .strict();

const generatedVaultItemSchema = z
  .object({
    vault_ref: z.string(),
    item_id: positiveID,
    project_id: positiveID,
    name: z.string(),
    secret_type: z.string(),
    status: z.string(),
    expires_at: z.string(),
    value_version: positiveID,
    metadata_revision: positiveID,
  })
  .strict();
const generatedVaultOutputSchema = z
  .object({
    item: generatedVaultItemSchema,
    secret_returned: z.literal(false),
  })
  .strict();
const vaultSessionOutputSchema = z
  .object({
    session_id: positiveID,
    session_generation: positiveID,
    runtime_id: positiveID,
    status: z.string(),
    environment_names: z.array(z.string()),
    expires_at: z.string(),
  })
  .strict();

const vaultActionStatuses = ["approval_pending", "running", "completed", "failed", "declined", "stale", "canceled", "expired"];
const vaultActionRecordedShape = {
  status: z.enum(vaultActionStatuses),
  request_id: positiveID,
  project_ref: z.string().min(1),
  reason: z.string().optional(),
  created_at: z.string().optional(),
  expires_at: z.string().optional(),
  secret_values_returned: z.literal(false),
  output_withheld: z.literal(true).optional(),
  retry_after_seconds: nonNegativeInteger.max(3600).optional(),
  assistant_hint: z.string().optional(),
  error: z.string().optional(),
};
const vaultActionRecordedSchema = z
  .discriminatedUnion("action_name", [
    z
      .object({
        ...vaultActionRecordedShape,
        action_name: z.literal("generate_item"),
        input: vaultGenerateInputSchema.optional(),
        output: generatedVaultOutputSchema.optional(),
      })
      .strict(),
    z
      .object({
        ...vaultActionRecordedShape,
        action_name: z.literal("restart_session_with_environment"),
        input: vaultSessionInputSchema.optional(),
        output: vaultSessionOutputSchema.optional(),
      })
      .strict(),
  ])
  .superRefine((value, context) => {
    if (value.output_withheld && value.output !== undefined) {
      context.addIssue({ code: z.ZodIssueCode.custom, message: "withheld Vault output must not contain result content" });
    }
  });
const vaultActionStoppedSchema = z.object({ status: z.literal("stopped"), error: z.string().min(1) }).strict();
const vaultActionResponseSchema = z.union([vaultActionRecordedSchema, vaultActionStoppedSchema]);

export const responseContracts = Object.freeze({
  connectorTargets: z.array(connectorTargetSchema),
  connectorHelp: connectorHelpSchema,
  connectorActions: connectorActionsSchema,
  connectorActionCall: connectorActionCallSchema,
  connectorActionRequest: connectorActionRequestSchema,
  vaultItems: vaultItemsSchema,
  vaultAction: vaultActionResponseSchema,
});

export function projectGatewaySuccess(schema, value, expected = undefined) {
  const result = schema.safeParse(value);
  if (!result.success) {
    throw gatewayContractError();
  }
  if (expected && result.data?.status !== "stopped") {
    for (const [field, expectedValue] of Object.entries(expected)) {
      if (expectedValue !== undefined && result.data?.[field] !== expectedValue) {
        throw gatewayContractError();
      }
    }
  } else if (expected && (expected.request_id !== undefined || expected.status !== undefined)) {
    throw gatewayContractError();
  }
  return result.data;
}

function gatewayContractError() {
  const error = new Error("Gateway success response failed MCP contract validation.");
  error.code = "gateway_response_contract_invalid";
  return error;
}
