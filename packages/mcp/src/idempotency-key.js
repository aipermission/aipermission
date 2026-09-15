import { Buffer } from "node:buffer";
import { z } from "zod";

export const idempotencyKeySchema = z
  .string()
  .min(1)
  .refine((value) => Buffer.byteLength(value, "utf8") <= 128, "Idempotency key must not exceed 128 UTF-8 bytes.")
  .describe("Caller-stable key used to prevent duplicate action requests.");
