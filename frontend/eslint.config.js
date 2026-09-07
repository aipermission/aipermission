import eslint from "@eslint/js";
import { createRequire } from "node:module";
import jsxA11y from "eslint-plugin-jsx-a11y-x";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";

const require = createRequire(import.meta.url);
const architecturePolicy = require("./architecture-policy.json");
const sourceExtensionGlob = architecturePolicy.sourceExtensions.map((extension) => extension.slice(1)).join(",");
const sourceFiles = [`src/**/*.{${sourceExtensionGlob}}`];
const nodeFiles = ["e2e/**/*.js", "scripts/**/*.mjs", "playwright.config.js", "vite.config.js", "vitest.config.js"];

export default [
  {
    ignores: ["dist/**", "node_modules/**", "playwright-report/**", "test-results/**"],
  },
  {
    ...eslint.configs.recommended,
    files: sourceFiles,
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      parserOptions: {
        ecmaFeatures: { jsx: true },
      },
      globals: globals.browser,
    },
    plugins: {
      "jsx-a11y-x": jsxA11y,
      "react-hooks": reactHooks,
    },
    settings: {
      "jsx-a11y-x": {
        components: {
          Button: "button",
          Checkbox: "input",
          Field: "label",
          Input: "input",
          Select: "select",
          Textarea: "textarea",
        },
      },
    },
    rules: {
      ...eslint.configs.recommended.rules,
      ...jsxA11y.configs.recommended.rules,
      "no-unused-vars": ["error", { argsIgnorePattern: "^_", varsIgnorePattern: "^_" }],
      complexity: ["error", { max: 25 }],
      "max-lines-per-function": ["error", { max: 250, skipBlankLines: true, skipComments: true, IIFEs: true }],
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "error",
    },
  },
  {
    ...eslint.configs.recommended,
    files: nodeFiles,
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
    rules: {
      ...eslint.configs.recommended.rules,
      "no-unused-vars": ["error", { argsIgnorePattern: "^_", varsIgnorePattern: "^_" }],
    },
  },
];
