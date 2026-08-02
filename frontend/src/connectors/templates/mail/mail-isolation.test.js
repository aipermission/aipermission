import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, extname, join, relative } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const sourceRoot = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");
const mailRoot = dirname(fileURLToPath(import.meta.url));
const mailIdentifiers = [
  "imap_", "smtp_", '"list_folders"', '"check_mailbox"', '"search_messages"',
  '"get_message"', '"list_attachments"', '"mark_read"', '"mark_unread"',
  '"move_message"', '"archive_message"', '"send_message"', '"reply_message"',
  '"delete_message"', "templates/mail/",
];

test("shared frontend runtime contains no Mail-specific connector branches", () => {
  const violations = [];
  for (const path of sourceFiles(sourceRoot)) {
    if (path.startsWith(`${mailRoot}/`) || path.endsWith(".test.js")) continue;
    const source = readFileSync(path, "utf8");
    const hasKindBranch = /(?:connector_kind|connectorKind|kind)\s*={2,3}\s*["']mail["']|["']mail["']\s*={2,3}\s*(?:connector_kind|connectorKind|kind)|(?:connector_kind|connectorKind|kind)\.includes\(["']mail["']\)|case\s+["']mail["']\s*:/.test(source);
    const hasSpecificIdentifier = mailIdentifiers.some((identifier) => source.toLowerCase().includes(identifier));
    if (hasKindBranch || hasSpecificIdentifier) {
      violations.push(relative(sourceRoot, path));
    }
  }
  assert.deepEqual(violations, []);
});

function sourceFiles(root) {
  const files = [];
  for (const entry of readdirSync(root, { withFileTypes: true })) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) files.push(...sourceFiles(path));
    else if ([".js", ".jsx"].includes(extname(path))) files.push(path);
  }
  return files;
}
