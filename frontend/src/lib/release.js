export const appVersion = "0.1.10";

export const changelogEntries = [
  {
    version: "0.1.10",
    label: "Console recovery controls",
    sections: [
      {
        title: "Added",
        items: [
          "Console shows the active long-running MCP command, running age, token label, command, and reason for the selected server.",
          "Local operators can restart a stuck persistent console session from the Console UI.",
          "MCP hints and operator instructions now describe the get_request, read_console, and restart_console_session recovery sequence.",
        ],
      },
      {
        title: "Fixed",
        items: [
          "Internal persistent-console prelude lines are hidden from the live console and MCP command output.",
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
          "MCP list_servers reports temporary grant expiration and omits expired grants.",
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
          "On-demand Docker quick checks from the Servers page.",
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
          "Database deletion now requires a second confirmation with the current password.",
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
