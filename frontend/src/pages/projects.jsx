import { Edit3, FolderKanban, Plus, RefreshCcw, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { Drawer } from "../components/ui/drawer";
import { Field, Input } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { apiDelete, apiGet, apiPost, apiPut } from "../lib/api";

const emptyEditor = { open: false, mode: "create", project: null, name: "" };

export function ProjectsPage() {
  const [projects, setProjects] = useState({ state: "loading", data: [], error: null });
  const [editor, setEditor] = useState(emptyEditor);
  const [remove, setRemove] = useState({ open: false, project: null });
  const [action, setAction] = useState({ state: "idle", message: "", error: null });

  useEffect(() => {
    void loadProjects();
  }, []);

  const totalTargets = useMemo(() => projects.data.reduce((total, project) => total + Number(project.target_count || 0), 0), [projects.data]);

  async function loadProjects() {
    setProjects((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/projects");
      setProjects({ state: "ready", data: data.items || [], error: null });
    } catch (error) {
      setProjects({ state: "error", data: [], error: error.message });
    }
  }

  function openCreate() {
    setAction({ state: "idle", message: "", error: null });
    setEditor({ open: true, mode: "create", project: null, name: "" });
  }

  function openEdit(project) {
    setAction({ state: "idle", message: "", error: null });
    setEditor({ open: true, mode: "edit", project, name: project.name });
  }

  async function saveProject(event) {
    event.preventDefault();
    setAction({ state: "saving", message: "", error: null });
    try {
      if (editor.mode === "edit") {
        await apiPut(`/api/projects/${editor.project.id}`, { name: editor.name });
      } else {
        await apiPost("/api/projects", { name: editor.name });
      }
      setEditor(emptyEditor);
      setAction({ state: "ready", message: editor.mode === "edit" ? "Project renamed." : "Project created.", error: null });
      await loadProjects();
    } catch (error) {
      setAction({ state: "error", message: "", error: error.message });
    }
  }

  async function archiveProject() {
    if (!remove.project) return;
    setAction({ state: "deleting", message: "", error: null });
    try {
      await apiDelete(`/api/projects/${remove.project.id}`);
      setRemove({ open: false, project: null });
      setAction({ state: "ready", message: "Project archived.", error: null });
      await loadProjects();
    } catch (error) {
      setAction({ state: "error", message: "", error: error.message });
    }
  }

  return (
    <section className="mx-auto grid w-full max-w-6xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">Projects</h3>
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

      <div className="overflow-hidden rounded-lg border border-stone-200 bg-white">
        <table className="w-full table-fixed border-collapse text-left text-sm">
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
                <td className="px-4 py-3"><Badge tone={project.target_count > 0 ? "good" : "neutral"}>{project.target_count}</Badge></td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-2">
                    <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Rename project" onClick={() => openEdit(project)}>
                      <Edit3 className="h-4 w-4" />
                    </Button>
                    <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Archive project" disabled={project.slug === "ungrouped" || project.target_count > 0} onClick={() => setRemove({ open: true, project })}>
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {projects.state === "loading" ? <div className="p-4"><Notice>Loading projects...</Notice></div> : null}
      </div>

      <Drawer open={editor.open} title={editor.mode === "edit" ? "Rename project" : "Add project"} description="Project names organize one developer's local connector workspace." onClose={() => setEditor(emptyEditor)}>
        <form className="grid gap-4" onSubmit={saveProject}>
          <Field>
            Project name
            <Input autoFocus value={editor.name} maxLength={80} onChange={(event) => setEditor((current) => ({ ...current, name: event.target.value }))} placeholder="My Project" />
          </Field>
          {action.error && editor.open ? <Notice tone="bad">{action.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={() => setEditor(emptyEditor)}>Cancel</Button>
            <Button type="submit" disabled={!editor.name.trim() || action.state === "saving"}>{action.state === "saving" ? "Saving..." : "Save project"}</Button>
          </div>
        </form>
      </Drawer>

      <Dialog open={remove.open} title="Archive project" description="Only empty projects can be archived." onClose={() => setRemove({ open: false, project: null })} size="md">
        <div className="grid gap-4">
          <Notice tone="warn">Archive {remove.project?.name}? Its stable project identity will no longer be available for new connector assignments.</Notice>
          {action.error && remove.open ? <Notice tone="bad">{action.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={() => setRemove({ open: false, project: null })}>Cancel</Button>
            <Button type="button" onClick={archiveProject} disabled={action.state === "deleting"}>{action.state === "deleting" ? "Archiving..." : "Archive project"}</Button>
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
