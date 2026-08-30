import eslint from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";

const sourceFiles = ["src/**/*.{js,jsx}"];
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
      "react-hooks": reactHooks,
    },
    rules: {
      ...eslint.configs.recommended.rules,
      "no-unused-vars": ["error", { argsIgnorePattern: "^_", varsIgnorePattern: "^_" }],
      complexity: ["error", { max: 50 }],
      "max-lines-per-function": ["error", { max: 500, skipBlankLines: true, skipComments: true, IIFEs: true }],
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "error",
    },
  },
  // Existing large functions are pinned to their current ceiling. New source
  // uses the stricter defaults above, and each override must only move down.
  {
    files: ["src/components/app-shell.jsx"],
    rules: {
      "max-lines-per-function": ["error", { max: 608, skipBlankLines: true, skipComments: true, IIFEs: true }],
    },
  },
  {
    files: ["src/components/file-transfer/file-transfer-dialog.jsx"],
    rules: {
      complexity: ["error", { max: 52 }],
      "max-lines-per-function": ["error", { max: 660, skipBlankLines: true, skipComments: true, IIFEs: true }],
    },
  },
  {
    files: ["src/connectors/templates/docker/console.jsx"],
    rules: {
      "max-lines-per-function": ["error", { max: 566, skipBlankLines: true, skipComments: true, IIFEs: true }],
    },
  },
  {
    files: ["src/connectors/templates/redis/console.jsx"],
    rules: {
      complexity: ["error", { max: 51 }],
    },
  },
  {
    files: ["src/connectors/templates/s3/console.jsx"],
    rules: {
      "max-lines-per-function": ["error", { max: 727, skipBlankLines: true, skipComments: true, IIFEs: true }],
    },
  },
  {
    files: ["src/connectors/templates/mail/console.jsx"],
    rules: {
      "max-lines-per-function": ["error", { max: 550, skipBlankLines: true, skipComments: true, IIFEs: true }],
    },
  },
  {
    files: ["src/pages/console.jsx"],
    rules: {
      complexity: ["error", { max: 80 }],
      "max-lines-per-function": ["error", { max: 702, skipBlankLines: true, skipComments: true, IIFEs: true }],
    },
  },
  {
    files: ["src/pages/unlock.jsx"],
    rules: {
      complexity: ["error", { max: 64 }],
    },
  },
  {
    files: ["src/pages/vault.jsx"],
    rules: {
      "max-lines-per-function": ["error", { max: 644, skipBlankLines: true, skipComments: true, IIFEs: true }],
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
