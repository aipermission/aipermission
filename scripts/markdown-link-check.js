#!/usr/bin/env node

const { execFileSync } = require("node:child_process");
const { existsSync, readFileSync, statSync } = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");

function trackedMarkdownFiles() {
  return execFileSync("git", ["ls-files", "-z", "*.md"], {
    cwd: root,
    encoding: "utf8",
  })
    .split("\0")
    .filter(Boolean);
}

function stripCode(markdown) {
  return markdown
    .replace(/^\s*(```|~~~)[\s\S]*?^\s*\1.*$/gm, "")
    .replace(/`[^`\n]*`/g, "");
}

function linkTargets(markdown) {
  const content = stripCode(markdown);
  const targets = [];
  for (const match of content.matchAll(/!?\[[^\]]*\]\(([^)]+)\)/g)) {
    targets.push(cleanTarget(match[1]));
  }
  for (const match of content.matchAll(/^\s*\[[^\]]+\]:\s*(\S+)/gm)) {
    targets.push(cleanTarget(match[1]));
  }
  return targets.filter(Boolean);
}

function cleanTarget(value) {
  let target = value.trim();
  if (target.startsWith("<") && target.includes(">")) {
    target = target.slice(1, target.indexOf(">"));
  } else {
    target = target.split(/\s+["']/u, 1)[0];
  }
  return target;
}

function isExternalTarget(target) {
  return /^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(target);
}

function decodeTarget(value) {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function anchorSlugs(markdown) {
  const slugs = new Set();
  const counts = new Map();
  for (const line of stripCode(markdown).split(/\r?\n/)) {
    const heading = line.match(/^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$/);
    if (heading) {
      const base = githubSlug(heading[1]);
      if (base) {
        const count = counts.get(base) || 0;
        slugs.add(count === 0 ? base : `${base}-${count}`);
        counts.set(base, count + 1);
      }
    }
    for (const anchor of line.matchAll(
      /<a\s+(?:id|name)=["']([^"']+)["'][^>]*>/gi,
    )) {
      slugs.add(anchor[1]);
    }
  }
  return slugs;
}

function githubSlug(value) {
  return stripInlineHTMLTags(value)
    .replace(/!?(?:\[([^\]]+)\])(?:\([^)]*\))?/g, "$1")
    .replace(/[`*_~]/g, "")
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s_-]/gu, "")
    .replace(/\s/g, "-");
}

function stripInlineHTMLTags(value) {
  let result = "";
  let insideTag = false;
  for (const character of value) {
    if (character === "<") {
      insideTag = true;
      continue;
    }
    if (insideTag) {
      if (character === ">") insideTag = false;
      continue;
    }
    result += character;
  }
  return result;
}

function checkMarkdownFiles(files, baseDir = root) {
  const findings = [];
  const anchorCache = new Map();
  for (const relativeFile of files) {
    const sourcePath = path.resolve(baseDir, relativeFile);
    const markdown = readFileSync(sourcePath, "utf8");
    for (const rawTarget of linkTargets(markdown)) {
      if (isExternalTarget(rawTarget)) continue;
      const [rawPath, rawAnchor = ""] = rawTarget.split("#", 2);
      const decodedPath = decodeTarget(rawPath).split("?", 1)[0];
      const decodedAnchor = decodeTarget(rawAnchor);
      const targetPath = decodedPath
        ? path.resolve(path.dirname(sourcePath), decodedPath)
        : sourcePath;
      const relativeTarget = path.relative(baseDir, targetPath);
      if (relativeTarget.startsWith("..") || path.isAbsolute(relativeTarget)) {
        findings.push(
          `${relativeFile}: local link escapes the repository: ${rawTarget}`,
        );
        continue;
      }
      if (!existsSync(targetPath)) {
        findings.push(
          `${relativeFile}: missing local link target: ${rawTarget}`,
        );
        continue;
      }
      if (!decodedAnchor || statSync(targetPath).isDirectory()) continue;
      let slugs = anchorCache.get(targetPath);
      if (!slugs) {
        slugs = anchorSlugs(readFileSync(targetPath, "utf8"));
        anchorCache.set(targetPath, slugs);
      }
      if (!slugs.has(decodedAnchor)) {
        findings.push(
          `${relativeFile}: missing anchor #${decodedAnchor} in ${path.relative(baseDir, targetPath)}`,
        );
      }
    }
  }
  return findings;
}

if (require.main === module) {
  const files = trackedMarkdownFiles();
  const findings = checkMarkdownFiles(files);
  if (findings.length > 0) {
    console.error("Markdown link check failed:");
    for (const finding of findings) console.error(`- ${finding}`);
    process.exit(1);
  }
  console.log(
    `Markdown link check passed (${files.length} tracked files checked).`,
  );
}

module.exports = { anchorSlugs, checkMarkdownFiles, githubSlug, linkTargets };
