package migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/db"
)

const Legacy010To020ID = "legacy_0_1_to_0_2"

type Server struct {
	config Config
	mux    *http.ServeMux
}

type migrationInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type statusResponse struct {
	Migrations []migrationInfo   `json:"migrations"`
	Databases  []db.DatabaseInfo `json:"databases"`
}

type migrateRequest struct {
	MigrationID      string `json:"migration_id"`
	SourceDatabaseID string `json:"source_database_id"`
	SourcePassword   string `json:"source_password"`
	TargetName       string `json:"target_name"`
	TargetPassword   string `json:"target_password"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewServer(config Config) *Server {
	server := &Server{config: config, mux: http.NewServeMux()}
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.index)
	s.mux.HandleFunc("GET /api/status", s.status)
	s.mux.HandleFunc("POST /api/migrate", s.migrate)
}

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTemplate.Execute(w, nil); err != nil {
		log.Printf("render migration page failed: %v", err)
	}
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	databases, err := db.ListDatabases(s.config.DataPath, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{
		Migrations: []migrationInfo{
			{
				ID:          Legacy010To020ID,
				Label:       "AIPermission 0.1.x to 0.2.0",
				Description: "Migrates SSH keys, servers, tokens, permissions, settings, and redaction rules into the connector-native 0.2 schema. History, audit, console sessions, and file transfer records are intentionally not copied.",
			},
		},
		Databases: databases,
	})
}

func (s *Server) migrate(w http.ResponseWriter, r *http.Request) {
	var request migrateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid json body"})
		return
	}
	request.MigrationID = strings.TrimSpace(request.MigrationID)
	if request.MigrationID == "" {
		request.MigrationID = Legacy010To020ID
	}
	if request.MigrationID != Legacy010To020ID {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "unsupported migration"})
		return
	}
	result, err := MigrateLegacy010To020(r.Context(), Legacy010To020Request{
		DataPath:         s.config.DataPath,
		FallbackSecret:   s.config.GatewaySecret,
		SourceDatabaseID: request.SourceDatabaseID,
		SourcePassword:   request.SourcePassword,
		TargetName:       request.TargetName,
		TargetPassword:   request.TargetPassword,
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrTargetExists) {
			status = http.StatusConflict
		}
		writeJSON(w, status, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write migration json failed: %v", err)
	}
}

var pageTemplate = template.Must(template.New("migration").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>AIPermission migration</title>
  <style>
    :root { color-scheme: dark; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #111; color: #f4f4f5; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #111; }
    main { width: min(880px, calc(100vw - 32px)); border: 1px solid #343434; background: #1f1f1f; border-radius: 8px; overflow: hidden; box-shadow: 0 24px 80px rgba(0,0,0,.35); }
    header { padding: 24px 28px; border-bottom: 1px solid #343434; display: flex; align-items: center; justify-content: space-between; gap: 18px; }
    h1 { margin: 0; font-size: 21px; }
    p { margin: 6px 0 0; color: #a1a1aa; line-height: 1.45; }
    form { padding: 24px 28px; display: grid; gap: 16px; }
    label { display: grid; gap: 7px; color: #d4d4d8; font-size: 13px; font-weight: 700; }
    input, select { width: 100%; box-sizing: border-box; border: 1px solid #3f3f46; background: #151515; color: #fafafa; border-radius: 6px; padding: 11px 12px; font: inherit; }
    .grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
    .notice { border: 1px solid #854d0e; background: #2d220d; color: #fde68a; padding: 12px 14px; border-radius: 6px; font-size: 13px; line-height: 1.45; }
    .toast { position: fixed; right: 20px; top: 20px; z-index: 20; display: none; border: 1px solid #3f3f46; background: #050505; color: #fafafa; border-radius: 6px; padding: 12px 14px; font-size: 13px; font-weight: 800; box-shadow: 0 18px 60px rgba(0,0,0,.4); }
    .toast.visible { display: block; }
    .actions { display: flex; align-items: center; gap: 12px; justify-content: flex-end; padding-top: 4px; }
    button { border: 1px solid #047857; background: #004231; color: white; padding: 11px 18px; border-radius: 6px; font-weight: 800; cursor: pointer; }
    button:disabled { opacity: .55; cursor: not-allowed; }
    pre { margin: 0; white-space: pre-wrap; word-break: break-word; border-top: 1px solid #343434; background: #151515; color: #d4d4d8; padding: 18px 28px; min-height: 80px; }
    code { color: #fef3c7; }
    @media (max-width: 720px) { .grid { grid-template-columns: 1fr; } header { align-items: flex-start; flex-direction: column; } }
  </style>
</head>
<body>
<div id="toast" class="toast" role="status" aria-live="polite"></div>
<main>
  <header>
    <div>
      <h1>AIPermission database migration</h1>
      <p>One-time, local-only migration helper. It creates a new database and never modifies the source database.</p>
    </div>
  </header>
  <form id="form">
    <div class="notice">
      Start this tool only when you need it: <code>docker compose --profile migrate up -d --build migration</code>.
      Then open <code>http://localhost:3211</code>. Stop it after migration.
      Do not use the source database in the normal gateway while migration is running.
    </div>
    <label>Migration
      <select id="migration_id" name="migration_id"></select>
    </label>
    <div class="grid">
      <label>Source database
        <select id="source_database_id" name="source_database_id"></select>
      </label>
      <label>Source password
        <input id="source_password" name="source_password" type="password" autocomplete="off" required />
      </label>
    </div>
    <div class="grid">
      <label>New database name
        <input id="target_name" name="target_name" placeholder="my-project-0-2" required />
      </label>
      <label>New database password
        <input id="target_password" name="target_password" type="password" autocomplete="new-password" required />
        <span>Use at least 14 characters with uppercase letters, lowercase letters, and numbers.</span>
      </label>
    </div>
    <div class="actions">
      <button id="submit" type="submit">Migrate database</button>
    </div>
  </form>
  <pre id="output">Loading migration status...</pre>
</main>
<script>
const form = document.getElementById("form");
const output = document.getElementById("output");
const submit = document.getElementById("submit");
const toast = document.getElementById("toast");
let toastTimer = 0;
function showToast(message) {
  toast.textContent = message;
  toast.classList.add("visible");
  window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => toast.classList.remove("visible"), 2600);
}
async function loadStatus() {
  const response = await fetch("/api/status");
  const data = await response.json();
  const migrations = document.getElementById("migration_id");
  migrations.innerHTML = "";
  for (const item of data.migrations || []) {
    const option = document.createElement("option");
    option.value = item.id;
    option.textContent = item.label;
    migrations.appendChild(option);
  }
  const dbs = document.getElementById("source_database_id");
  dbs.innerHTML = "";
  for (const item of data.databases || []) {
    const option = document.createElement("option");
    option.value = item.id;
    option.textContent = item.name + " (" + item.id + ")";
    dbs.appendChild(option);
  }
  output.textContent = data.databases?.length ? "Ready. Choose a source database and create a new 0.2 database." : "No source databases found.";
}
form.addEventListener("submit", async (event) => {
  event.preventDefault();
  submit.disabled = true;
  output.textContent = "Migrating...";
  const payload = Object.fromEntries(new FormData(form).entries());
  try {
    const response = await fetch("/api/migrate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
    const data = await response.json();
    if (!response.ok) {
      output.textContent = data.error || "Migration failed.";
      return;
    }
    output.textContent = [
      "Migration summary",
      "",
      "New database: " + data.target_database_name + " (" + data.target_database_id + ")",
      "SSH keys: " + data.ssh_keys,
      "SSH connector targets: " + data.targets,
      "Tokens: " + data.tokens,
      "Permissions: " + data.permissions,
      "",
      "Stop this migration service after you verify the new database."
    ].join("\n");
    showToast("Migration completed.");
    await loadStatus();
  } catch (error) {
    output.textContent = String(error);
  } finally {
    submit.disabled = false;
  }
});
loadStatus().catch((error) => { output.textContent = String(error); });
</script>
</body>
</html>`))

func MigrationStartMessage(port string) string {
	return fmt.Sprintf("To migrate an older database, run `docker compose --profile migrate up -d --build migration`, then open http://localhost:%s.", port)
}
