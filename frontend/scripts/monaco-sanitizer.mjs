import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { normalizePath } from "vite";

const require = createRequire(import.meta.url);

// Monaco vendors a second sanitizer; an npm override alone does not replace it.
export function monacoSanitizer() {
  const consumer = normalizePath(require.resolve("monaco-editor/base/browser/domSanitize"));
  const vendored = normalizePath(require.resolve("monaco-editor/base/browser/dompurify/dompurify"));
  const replacement = normalizePath(fileURLToPath(import.meta.resolve("dompurify")));
  const matches = (id, file) => {
    const normalized = normalizePath(String(id || ""));
    return normalized === file || normalized.startsWith(`${file}?`) || normalized.startsWith(`${file}#`);
  };
  const redirect = (source, importer) => (source === "./dompurify/dompurify.js" && matches(importer, consumer) ? replacement : null);
  return {
    name: "aipermission-monaco-sanitizer",
    enforce: "pre",
    config() {
      return {
        optimizeDeps: {
          esbuildOptions: {
            plugins: [
              {
                name: "aipermission-monaco-sanitizer",
                setup(build) {
                  build.onResolve({ filter: /^\.\/dompurify\/dompurify\.js$/ }, (args) => {
                    const path = redirect(args.path, args.importer);
                    return path ? { path } : undefined;
                  });
                },
              },
            ],
          },
        },
      };
    },
    resolveId(source, importer) {
      return redirect(source, importer);
    },
    generateBundle() {
      const modules = [...this.getModuleIds()];
      const includes = (file) => modules.some((id) => matches(id, file));
      if (includes(vendored) || (includes(consumer) && !includes(replacement))) {
        this.error("Monaco must use the pinned DOMPurify dependency, not its embedded sanitizer");
      }
    },
  };
}
