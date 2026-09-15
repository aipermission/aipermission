import { Edit3, FolderKanban, Plus, RefreshCcw, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { Drawer } from "../components/ui/drawer";
import { Field, Input } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { apiDelete, apiGet, apiPost, apiPut } from "../lib/api";
import { useRequestGuard } from "../lib/request-guard";

const emptyEditor = { open: false, mode: "create", project: null, name: "" };

export function ProjectsPage() {
  const [projects, setProjects] = useState({ state: "loading", data: [], error: null });
  const [editor, setEditor] = useState(emptyEditor);
  const [remove, setRemove] = useState({ open: false, project: null });
  const [action, setAction] = useState({ state: "idle", message: "", error: null });
  const requests = useRequestGuard("projects");

  const totalTargets = useMemo(
    () => projects.data.reduce((total, project) => total + Number(project.target_count || 0), 0),
    [projects.data],
  );

  const loadProjects = useCallback(async () => {
    const request = requests.begin("list");
    setProjects((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/projects", { signal: request.signal });
      if (!request.isCurrent()) return;
      setProjects({ state: "ready", data: data.items || [], error: null });
    } catch (error) {
      if (request.isCurrent()) setProjects({ state: "error", data: [], error: error.message });
    } finally {
      request.complete();
    }
  }, [requests]);

  useEffect(() => {
    void loadProjects();
  }, [loadProjects]);

  function openCreate() {
    requests.invalidate("editor");
    setAction({ state: "idle", message: "", error: null });
    setEditor({ open: true, mode: "create", project: null, name: "" });
  }

  function openEdit(project) {
    requests.invalidate("editor");
    setAction({ state: "idle", message: "", error: null });
    setEditor({ open: true, mode: "edit", project, name: project.name });
  }

  function closeEditor() {
    requests.invalidate("editor");
    setEditor(emptyEditor);
    setAction({ state: "idle", message: "", error: null });
  }

  function closeArchive() {
    requests.invalidate("archive");
    setRemove({ open: false, project: null });
    setAction({ state: "idle", message: "", error: null });
  }

  async function saveProject(event) {
    event.preventDefault();
    const draft = editor;
    const request = requests.begin("editor");
    setAction({ state: "saving", message: "", error: null });
    try {
      if (draft.mode === "edit") {
        await apiPut(`/api/projects/${draft.project.id}`, { name: draft.name }, { signal: request.signal });
      } else {
        await apiPost("/api/projects", { name: draft.name }, { signal: request.signal });
      }
      if (request.isCurrent()) {
        setEditor(emptyEditor);
        setAction({ state: "ready", message: draft.mode === "edit" ? "Project renamed." : "Project created.", error: null });
      }
      await loadProjects();
    } catch (error) {
      if (request.isCurrent()) setAction({ state: "error", message: "", error: error.message });
    } finally {
      request.complete();
    }
  }

  async function archiveProject() {
    if (!remove.project) return;
    const project = remove.project;
    const request = requests.begin("archive");
    setAction({ state: "deleting", message: "", error: null });
    try {
      await apiDelete(`/api/projects/${project.id}`, { signal: request.signal });
      if (request.isCurrent()) {
        setRemove({ open: false, project: null });
        setAction({ state: "ready", message: "Project archived.", error: null });
      }
      await loadProjects();
    } catch (error) {
      if (request.isCurrent()) setAction({ state: "error", message: "", error: error.message });
    } finally {
      request.complete();
    }
  }

  return (
    <section className="mx-auto grid w-full max-w-6xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">Projects</h1>
          <p className="text-sm text-stone-500">Organize connector targets and control which projects each AI token can see.</p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" onClick={loadProjects} disabled={projects.state === "loading"}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
          <Button type="button" onClick={openCreate}>
            <Plus className="h-4 w-4" />
            Add project
          </Button>
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <ProjectStat label="Projects" value={projects.data.length} />
        <ProjectStat label="Connector targets" value={totalTargets} />
        <ProjectStat label="Ungrouped" value={projects.data.find((project) => project.slug === "ungrouped")?.target_count || 0} />
      </div>

      {projects.state === "error" ? <Notice tone="bad">{projects.error}</Notice> : null}
      {action.message ? <Notice tone="good">{action.message}</Notice> : null}
      {action.error && !editor.open && !remove.open ? <Notice tone="bad">{action.error}</Notice> : null}

      <div className="overflow-x-auto rounded-lg border border-stone-200 bg-white">
        <table className="w-full min-w-[650px] table-fixed border-collapse text-left text-sm">
          <thead className="bg-stone-50 text-xs uppercase text-stone-500">
            <tr>
              <th className="w-[42%] px-4 py-3 font-semibold">Project</th>
              <th className="w-[28%] px-4 py-3 font-semibold">Stable slug</th>
              <th className="w-[15%] px-4 py-3 font-semibold">Targets</th>
              <th className="w-[15%] px-4 py-3 text-right font-semibold">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-stone-200">
            {projects.data.map((project) => (
              <tr key={project.id} className="hover:bg-stone-50">
                <td className="px-4 py-3">
                  <span className="flex min-w-0 items-center gap-2 font-semibold text-stone-950">
                    <FolderKanban className="h-4 w-4 shrink-0 text-stone-500" />
                    <span className="truncate">{project.name}</span>
                    {project.slug === "ungrouped" ? <Badge tone="neutral">default</Badge> : null}
                  </span>
                </td>
                <td className="truncate px-4 py-3 font-mono text-xs text-stone-500">{project.slug}</td>
                <td className="px-4 py-3">
                  <Badge tone={project.target_count > 0 ? "good" : "neutral"}>{project.target_count}</Badge>
                </td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      className="h-9 w-9 px-0"
                      title="Rename project"
                      onClick={() => openEdit(project)}
                    >
                      <Edit3 className="h-4 w-4" />
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      className="h-9 w-9 px-0"
                      title="Archive project"
                      disabled={project.slug === "ungrouped" || project.target_count > 0}
                      onClick={() => {
                        requests.invalidate("archive");
                        setRemove({ open: true, project });
                      }}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {projects.state === "loading" ? (
          <div className="p-4">
            <Notice>Loading projects...</Notice>
          </div>
        ) : null}
      </div>

      <Drawer
        open={editor.open}
        title={editor.mode === "edit" ? "Rename project" : "Add project"}
        description="Project names organize one developer's local connector workspace."
        onClose={closeEditor}
      >
        <form className="grid gap-4" onSubmit={saveProject}>
          <Field>
            Project name
            <Input
              value={editor.name}
              maxLength={80}
              onChange={(event) => {
                requests.invalidate("editor");
                setEditor((current) => ({ ...current, name: event.target.value }));
                setAction({ state: "idle", message: "", error: null });
              }}
              placeholder="My Project"
            />
          </Field>
          {action.error && editor.open ? <Notice tone="bad">{action.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={closeEditor}>
              Cancel
            </Button>
            <Button type="submit" disabled={!editor.name.trim() || action.state === "saving"}>
              {action.state === "saving" ? "Saving..." : "Save project"}
            </Button>
          </div>
        </form>
      </Drawer>

      <Dialog
        open={remove.open}
        title="Archive project"
        description="Only empty projects can be archived."
        onClose={closeArchive}
        size="md"
      >
        <div className="grid gap-4">
          <Notice tone="warn">
            Archive {remove.project?.name}? Its stable project identity will no longer be available for new connector assignments.
          </Notice>
          {action.error && remove.open ? <Notice tone="bad">{action.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={closeArchive}>
              Cancel
            </Button>
            <Button type="button" onClick={archiveProject} disabled={action.state === "deleting"}>
              {action.state === "deleting" ? "Archiving..." : "Archive project"}
            </Button>
          </div>
        </div>
      </Dialog>
    </section>
  );
}

function ProjectStat({ label, value }) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border border-stone-200 bg-white p-4">
      <span className="text-sm font-medium text-stone-500">{label}</span>
      <Badge tone="neutral">{value}</Badge>
    </div>
  );
}
