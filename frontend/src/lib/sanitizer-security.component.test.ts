import DOMPurify from "dompurify";
import { describe, expect, it } from "vitest";

describe("pinned sanitizer security", () => {
  it.each(["afterSanitizeElements", "afterSanitizeAttributes"] as const)("neutralizes descendants detached by %s", (hook) => {
    const purifier = DOMPurify(window);
    const root = document.createElement("div");
    root.innerHTML = '<section id="detached"><img src="x" onerror="UNTRUSTED()"></section>';
    const descendant = root.querySelector("img")!;
    document.body.append(root);
    try {
      const detach = (node: Node) => {
        if (node instanceof Element && node.id === "detached") node.remove();
      };
      if (hook === "afterSanitizeElements") purifier.addHook(hook, detach);
      else purifier.addHook(hook, detach);
      purifier.sanitize(root, { IN_PLACE: true });
      expect(root.querySelector("section")).toBeNull();
      expect(descendant.hasAttribute("onerror")).toBe(false);
    } finally {
      purifier.removeAllHooks();
      root.remove();
    }
  });
});
