export const appVersion = "0.1.2";

export const changelogEntries = [
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
