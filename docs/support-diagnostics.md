# Support Diagnostics

AIPermission can export a bounded JSON diagnostics report from **Settings >
Support diagnostics**. The report is intended for troubleshooting a local
installation without collecting connector payloads or credential material.

The download requires an active local UI session. Generating it records a
metadata-only Audit event; the report itself is not stored by AIPermission.

## Included Data

The report uses a strict allowlist and currently includes:

- AIPermission, Go, operating-system, architecture, SQLCipher, schema, and
  connector version metadata
- coarse gateway, database, MCP, and audit-outbox health
- aggregate counts for active actions, approvals, consoles, transfers, and
  unknown connector outcomes
- at most 20 grouped error summaries derived from the latest 200 failed History
  records

Error summaries contain only connector kind, activity type, status, a bounded
category, count, and timestamp. Raw error text is never exported.

## Excluded Data

The report excludes by design:

- credentials, Vault values, private keys, API tokens, and session secrets
- endpoints, addresses, target names, profile names, and database names
- commands, action payloads, message content, raw output, and raw errors
- local private paths

This boundary is covered by a golden redaction test containing sentinel
credentials, names, addresses, commands, output, and paths.

## Local Delivery Deadline Evidence

When a delivery guard reaches its deadline, the backend can write a bounded
`delivery database deadline diagnostics` record to its local service log. This
contains pool counts, cumulative pool-wait totals, and allowlisted function
names derived from program counters. It never formats function arguments,
context values, SQL text, SQL parameters, filesystem paths, or raw errors.
Records are limited to one every 30 seconds per unlocked workspace, at most
eight grouped code stacks and sixteen function names per group. A truncated
capture is marked explicitly.

Pool counts belong to the affected database; `code_scope: "process"` means the
code-location sample spans the process and cannot by itself attribute a stack
to a particular database. The record is investigation evidence, not proof of
the connection holder or a successful authorization. No permission, timeout,
pool-size or fail-closed behavior is relaxed. These records are not added to
the downloadable JSON report. Other service log lines may contain different
information, so do not share the entire log without reviewing it.

## Sharing A Report

Diagnostics are designed to be safe to inspect and selectively share, but no
automatic redaction system should replace human review. Open the JSON file and
review it before attaching it to a public issue. Share the smallest report
needed to reproduce the problem.

Do not attach the encrypted `.aipdb` database, database password, backup
credentials, connector credentials, private keys, or terminal transcripts to a
public issue.

## REST Endpoint

The local UI calls `GET /api/settings/diagnostics`. The response is an
attachment with `Cache-Control: no-store`. See the [REST API
reference](api/rest-api.md) for the typed response contract.
