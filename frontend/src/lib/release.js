export const appVersion = "0.2.2";

export const changelogEntries = [
  {
    version: "0.2.2",
    label: "Postgres management",
    sections: [
      {
        title: "Added",
        items: [
          "Postgres connector profiles can provision managed database users with schema, table, and column scopes.",
          "Postgres connector profiles can run local SQL backup/download and restore/upload flows through pg_dump and psql.",
          "The Postgres console includes a schema browser with table expansion, column metadata, and SQL autocomplete metadata from pg_catalog.",
        ],
      },
      {
        title: "Changed",
        items: [
          "Managed Postgres credential profiles keep their generated username fixed and clean up the managed database role when the profile is deleted.",
          "Postgres backup uses a newer PostgreSQL client in the backend image to avoid server/client dump-version mismatches.",
        ],
      },
    ],
  },
  {
    version: "0.2.1",
    label: "Maintenance hardening",
    sections: [
      {
        title: "Changed",
        items: [
          "Refreshed backend and frontend base image digests and dependency groups through Dependabot maintenance updates.",
          "Updated the MCP package metadata to 0.2.1 for the maintenance release.",
          "Kept monaco-editor on the audit-clean 0.53 line until the newer line clears its transitive advisory.",
        ],
      },
      {
        title: "Security",
        items: [
          "Updated golang.org/x/crypto to 0.53.0.",
          "Updated the MCP transitive Hono resolution to a non-vulnerable version.",
          "Hardened SSH connector integer config parsing with native-int bounds checks.",
        ],
      },
    ],
  },
  {
    version: "0.2.0",
    label: "Connector-native baseline",
    sections: [
      {
        title: "Added",
        items: [
          "SSH and Postgres now run through the same connector target, credential profile, token action permission, approval, history, and audit pipeline.",
          "Connector UI templates define target forms, credential forms, list operations, console/activity surfaces, and toolbar actions per connector kind.",
          "Postgres connector actions support schema discovery, table metadata, and bounded read-only SQL through database credential profiles.",
          "Runtime-backed connector capabilities use typed adapter contracts for live terminals, file transfer, credential resources, and lifecycle cleanup.",
        ],
      },
      {
        title: "Changed",
        items: [
          "The local database schema is reset as a clean 0.2 connector-native baseline while the project is still pre-1.0.",
          "Pre-0.2 preview databases are not opened directly by the normal gateway. Create a fresh 0.2 database, or use the local migration helper for important 0.1.x data.",
          "Postgres targets default to SSL require; weaker modes are an explicit local-lab choice.",
        ],
      },
      {
        title: "Security",
        items: [
          "Connector approvals include context snapshots for target/profile metadata, credential revisions, connector action definitions, permission state, and prepared payload hashes.",
          "Stale approvals record a coarse drift reason such as token, permission, target, profile, action definition, or payload drift.",
          "Structured connector outputs use shared redaction before MCP responses, history, and audit persistence.",
        ],
      },
    ],
  },
  {
    version: "0.1.14",
    label: "AGPL licensing",
    sections: [
      {
        title: "Changed",
        items: [
          "AIPermission is licensed under AGPL-3.0-only from v0.1.14 onward.",
          "Versions up to and including v0.1.13 remain available under their original MIT license.",
        ],
      },
    ],
  },
  {
    version: "0.1.13",
    label: "MCP multi-server commands",
    sections: [
      {
        title: "Added",
        items: [
          "MCP exec can run the same command across multiple visible SSH targets while keeping per-target request, approval, output, and error records.",
          "MCP read_console can inspect several always-run SSH target consoles in one read-only call.",
          "MCP command responses can include basic policy warnings for common high-risk command patterns.",
        ],
      },
      {
        title: "Fixed",
        items: [
          "NAS and appliance prompt detection now recognizes bracket-style shell prompts such as [~] #.",
        ],
      },
      {
        title: "Security",
        items: [
          "Multi-server MCP execution still respects per-server permissions, approval-required prompts, blocked rules, approval-context drift checks, and token-scoped history.",
          "Policy warnings are best-effort safety rails and do not replace local operator review.",
        ],
      },
    ],
  },
  {
    version: "0.1.12",
    label: "Bulk console commands",
    sections: [
      {
        title: "Added",
        items: [
          "Console Bulk command can run one shell command across multiple selected SSH targets from the local UI.",
          "Bulk command requires an explicit confirmation phrase and records one manual command request per target server.",
          "Bulk results show per-server status with selectable captured output and compact target search.",
        ],
      },
      {
        title: "Security",
        items: [
          "Bulk command remains local UI only and requires the unlocked UI session plus CSRF checks.",
          "Bulk command history stays separate from MCP token-scoped approval history.",
        ],
      },
    ],
  },
  {
    version: "0.1.11",
    label: "SSH compatibility polish",
    sections: [
      {
        title: "Added",
        items: [
          "SSH connectors can store optional advanced startup settings for NAS and appliance targets that show a menu before shell access.",
          "Advanced startup settings can send post-connect input such as q plus newline, or start a specific shell command when needed.",
        ],
      },
      {
        title: "Fixed",
        items: [
          "Windows checkouts preserve LF line endings for Docker shell entrypoints.",
          "Console recovery banners distinguish manual commands from MCP/AI commands.",
          "SSH command, Docker check, and connection-test failures now show clearer timeout, refused, auth, and host-key messages.",
          "Approval Run checks SSH session readiness before closing the prompt.",
          "Basic redaction no longer masks normal shell PWD=/path output.",
        ],
      },
    ],
  },
  {
    version: "0.1.10",
    label: "Console recovery controls",
    sections: [
      {
        title: "Added",
        items: [
          "Console shows the active long-running MCP command, running age, token label, command, and reason for the selected server.",
          "Local operators can restart a stuck persistent console session from the Console UI.",
          "MCP hints and operator instructions now describe the get_connector_action_request, read_console, and restart_console_session recovery sequence.",
        ],
      },
      {
        title: "Fixed",
        items: [
          "Internal persistent-console prelude lines are hidden from the live console and MCP command output.",
          "MCP server-list hints clarify that agents should use exec connection errors as the current reachability signal.",
        ],
      },
    ],
  },
  {
    version: "0.1.9",
    label: "Approval drift and console recovery",
    sections: [
      {
        title: "Added",
        items: [
          "Pending MCP approvals now store an approval-context snapshot and become stale if the context changes before Run.",
          "Approval dialogs show how long ago the request was created.",
          "MCP clients can restart a stuck persistent console session so the next exec opens a fresh SSH session.",
        ],
      },
      {
        title: "Fixed",
        items: [
          "Persistent console MCP exec is more resilient across repeated commands and closed sessions.",
        ],
      },
    ],
  },
  {
    version: "0.1.8",
    label: "Temporary permissions",
    sections: [
      {
        title: "Added",
        items: [
          "Token/server permissions can expire after a temporary maintenance window.",
          "Console token controls can set active Prompt or Always permissions to 1 hour, 4 hours, or 1 day.",
          "The always-run warning shows a countdown when an active Always grant is temporary.",
          "MCP target discovery reports temporary grant expiration and omits expired grants.",
        ],
      },
      {
        title: "Security",
        items: [
          "Expired token/server permissions no longer authorize MCP command, console, file-transfer, or server-list access.",
          "Permission expiration is a local safety rail; it does not make LAN or public exposure safe.",
        ],
      },
    ],
  },
  {
    version: "0.1.7",
    label: "MCP transfer tools",
    sections: [
      {
        title: "Added",
        items: [
          "MCP tools can list token-scoped file transfer status and batch progress.",
          "MCP can browse remote directories and start remote download queues for always-run server permissions.",
          "MCP can save completed downloads to explicit local paths and upload explicit local files.",
          "Prompt-required MCP transfers appear in Transfer Center so selected files can be approved and the rest rejected with a note.",
          "MCP can pause, resume, and cancel active transfer queues.",
          "Transfer Center shows active and recent UI/MCP transfer queues from the sidebar.",
        ],
      },
      {
        title: "Security",
        items: [
          "MCP transfer responses never include local temp paths, archive staging paths, local upload paths, or file contents.",
          "MCP direct upload/download tools require explicit paths; prompt mode stages files locally until the operator approves them.",
        ],
      },
    ],
  },
  {
    version: "0.1.6",
    label: "Bulk file transfer queues",
    sections: [
      {
        title: "Added",
        items: [
          "Queued SSH/SFTP uploads and downloads from the local Console UI.",
          "Pause and resume for active transfers while the gateway process stays running.",
          "Per-file status, speed, ETA, and progress for transfer queues.",
          "Multi-file downloads are packaged as a local zip after the remote downloads complete.",
          "Duplicate queue paths are rejected before transfer start.",
        ],
      },
      {
        title: "Security",
        items: [
          "File contents still stay out of SQLCipher; AIPermission persists transfer metadata and short-lived local staging files only.",
          "Uploads are staged on the remote server and moved into place only after completion, so canceled uploads do not leave partial target files.",
          "Download batches are capped at 1 GiB total remote file size.",
          "MCP file transfer tools remain intentionally unavailable while UI transfer safety and audit semantics are dogfooded.",
        ],
      },
    ],
  },
  {
    version: "0.1.5",
    label: "SSH file transfers",
    sections: [
      {
        title: "Added",
        items: [
          "Upload one local file to a selected server over SFTP.",
          "Download one remote file through the local gateway after transfer completion.",
          "Remote file browser for selecting upload folders and download files.",
          "Cancel pending or running UI file transfers.",
          "Overwrite confirmation before replacing an existing remote file.",
          "File Transfer History with status, live progress, checksum, server, and path metadata.",
        ],
      },
      {
        title: "Security",
        items: [
          "File contents are staged in a private temporary directory and are never stored in SQLCipher.",
          "The current release exposes file transfer from the local web UI only; MCP file transfer tools are not exposed yet.",
        ],
      },
    ],
  },
  {
    version: "0.1.4",
    label: "Manual console history",
    sections: [
      {
        title: "Added",
        items: [
          "Manual Console command logging for typed or pasted terminal input.",
          "Best-effort output capture for simple manual commands when the shell prompt returns.",
          "History source filters and badges for MCP and manual command records.",
        ],
      },
      {
        title: "Security",
        items: [
          "MCP request APIs now explicitly stay scoped to MCP-origin command requests.",
          "Manual Console History does not install shell hooks or hidden command suffixes.",
          "Interactive commands, nested shells, and unsafe control sequences stay untracked; arrow/history recall uses a placeholder command when output can be captured.",
        ],
      },
    ],
  },
  {
    version: "0.1.3",
    label: "SSH key and host import",
    sections: [
      {
        title: "Added",
        items: [
          "Import existing SSH private keys into the local encrypted vault.",
          "Import SSH host entries from OpenSSH config files or pasted config content.",
        ],
      },
      {
        title: "Changed",
        items: [
          "Command, output, log, and setup code blocks now use consistent terminal typography.",
          "SSH host import avoids sending IdentityFile paths into server descriptions.",
        ],
      },
    ],
  },
  {
    version: "0.1.2",
    label: "History labels and Docker checks",
    sections: [
      {
        title: "Added",
        items: [
          "History labels for tagging command requests and filtering History by label.",
          "History label cleanup from Settings without deleting command history records.",
          "On-demand Docker quick checks from SSH connector targets.",
          "Docker container details and tail-configurable Docker logs dialogs.",
        ],
      },
    ],
  },
  {
    version: "0.1.1",
    label: "Dogfooding polish",
    sections: [
      {
        title: "Added",
        items: [
          "Manual update checks from the Changelog dialog.",
          "Bulk token permission updates across all servers.",
          "Optional approval-run notes that are delivered back to the AI.",
        ],
      },
      {
        title: "Changed",
        items: [
          "Console side panels can collapse for narrower screens.",
          "Browser title shows MCP runtime state and active database name after unlock.",
          "Database deletion now uses the unlock form password, then asks for the database name before deleting.",
        ],
      },
    ],
  },
  {
    version: "0.1.0-rc.1",
    label: "Public RC",
    sections: [
      {
        title: "Added",
        items: [
          "Local-only Docker gateway with React UI on http://localhost:3210.",
          "SQLCipher-encrypted named databases with unlock, switch, import, backup, rename, delete, and password-change flows.",
          "Gateway-owned SSH keys, SSH host fingerprint approval, token-scoped MCP execution, approvals, console sessions, history, and audit logs.",
        ],
      },
      {
        title: "Security",
        items: [
          "Private SSH keys and reusable token values stay inside the local encrypted gateway.",
          "API tokens are stored as hashes and shown once by default.",
          "Approval-required raw commands are encrypted separately so display redaction cannot mutate execution payloads.",
        ],
      },
    ],
  },
];
