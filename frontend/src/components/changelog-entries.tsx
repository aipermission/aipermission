import release from "../lib/release.generated.json" with { type: "json" };
import { Badge } from "./ui/badge";

export function ChangelogEntries() {
  return (
    <>
      {release.entries.map((entry) => (
        <section key={entry.version} className="grid gap-3">
          <div className="flex items-center justify-between gap-3 border-b border-stone-200 pb-2">
            <h3 className="text-sm font-semibold text-stone-950">{entry.version}</h3>
            <Badge>{entry.label}</Badge>
          </div>
          {entry.sections.map((section) => (
            <div key={section.title} className="grid gap-2">
              <h4 className="text-xs font-semibold uppercase text-stone-500">{section.title}</h4>
              <ul className="grid gap-2 text-sm text-stone-700">
                {section.items.map((item) => (
                  <li key={item} className="rounded-md border border-stone-200 bg-stone-50 px-3 py-2">
                    {item}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </section>
      ))}
    </>
  );
}
