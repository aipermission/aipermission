import { z } from "zod";
import { idempotencyKeySchema } from "./idempotency-key.js";

export const listVaultItemsSchema = {
  project_ref: z
    .string()
    .trim()
    .min(1)
    .optional()
    .describe("Optional project reference. Prefer id:<id> or slug:<slug>; omit to list every readable project."),
};

export const callVaultActionSchema = {
  project_ref: z.string().trim().min(1).describe("Owning project reference. Prefer id:<id> or slug:<slug>."),
  action_name: z.enum(["generate_item", "restart_session_with_environment"]),
  input: z.record(z.unknown()).describe("Action input. Never include raw secret values."),
  reason: z.string().min(1).describe("Why this Vault action is needed."),
  idempotency_key: idempotencyKeySchema,
};

export const vaultActionRequestSchema = {
  request_id: z.number().int().positive().describe("Request id returned by call_vault_action."),
};
