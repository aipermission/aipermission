import { Edit3, Eye, Link2, RotateCw, Trash2 } from "lucide-react";
import { formatRelativeAge } from "../../lib/date-time";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { vaultSecretTypes } from "./vault-options";

export function VaultRow({ item, projects, onEdit, onReveal, onReplace, onBindings, onDelete }) {
  const projectNames = [
    item.owner_project_name,
    ...(item.project_ids || []).map((id) => projects.find((project) => Number(project.id) === Number(id))?.name).filter(Boolean),
  ];
  const visibleTags = (item.tags || []).slice(0, 3);
  const hiddenTagCount = Math.max(0, (item.tags || []).length - visibleTags.length);
  const expiry = expiryState(item);
  return (
    <tr className="hover:bg-stone-50">
      <td className="px-4 py-3">
        <p className="truncate font-mono text-xs font-semibold text-stone-950">{item.name}</p>
        <p className="mt-1 truncate text-xs text-stone-500">
          {[item.provider, item.environment].filter(Boolean).join(" / ") || "Local secret"}
        </p>
        {visibleTags.length > 0 ? (
          <div className="mt-2 flex min-w-0 flex-wrap gap-1" title={(item.tags || []).join(", ")}>
            {visibleTags.map((tag) => (
              <Badge key={tag} tone="neutral" className="max-w-32 truncate px-2 py-0.5 font-medium">
                {tag}
              </Badge>
            ))}
            {hiddenTagCount > 0 ? (
              <Badge tone="neutral" className="px-2 py-0.5 font-medium">
                +{hiddenTagCount}
              </Badge>
            ) : null}
          </div>
        ) : null}
      </td>
      <td className="px-4 py-3">
        <Badge tone="neutral">{secretTypeLabel(item.secret_type)}</Badge>
      </td>
      <td className="px-4 py-3">
        <div className="flex flex-wrap gap-1">
          {projectNames.map((name, index) => (
            <Badge key={`${name}-${index}`} tone={index === 0 ? "good" : "neutral"}>
              {name}
            </Badge>
          ))}
        </div>
      </td>
      <td className="px-4 py-3">
        <Badge tone={expiry.tone}>{expiry.label}</Badge>
      </td>
      <td className="px-4 py-3 text-xs text-stone-500">{item.last_used_at ? formatRelativeAge(item.last_used_at) : "Never"}</td>
      <td className="px-4 py-3">
        <div className="flex justify-end gap-2">
          <IconButton title="Default session bindings" icon={Link2} onClick={onBindings} />
          <IconButton title="Reveal and copy" icon={Eye} onClick={onReveal} />
          <IconButton title="Replace local value" icon={RotateCw} onClick={onReplace} />
          <IconButton title="Edit metadata" icon={Edit3} onClick={onEdit} />
          <IconButton title="Delete" icon={Trash2} onClick={onDelete} />
        </div>
      </td>
    </tr>
  );
}

function IconButton({ title, icon: Icon, onClick }) {
  return (
    <Button type="button" variant="outline" className="h-9 w-9 px-0" title={title} onClick={onClick}>
      <Icon className="h-4 w-4" />
    </Button>
  );
}

function secretTypeLabel(value) {
  return vaultSecretTypes.find(([type]) => type === value)?.[1] || value;
}

function expiryState(item) {
  if (!item.expires_at) return { tone: "neutral", label: "Never" };
  const expiresAt = Date.parse(item.expires_at);
  const now = Date.now();
  if (expiresAt <= now) return { tone: "bad", label: "Expired" };
  const days = Math.max(1, Math.ceil((expiresAt - now) / 86400000));
  if (days <= Number(item.expiry_warning_days || 14)) return { tone: "warn", label: `${days}d left` };
  return { tone: "good", label: new Date(expiresAt).toLocaleDateString() };
}
