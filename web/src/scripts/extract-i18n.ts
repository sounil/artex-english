/**
 * Script: extract-i18n.ts
 *
 * ARTEX localization — first-pass UI string extractor.
 *
 * Parses every `.ts`/`.tsx` under `src/` with the TypeScript AST and extracts only the
 * user-facing hard-coded Chinese strings. There are three kinds of targets:
 *   1) string literals          e.g. title: "仪表盘"
 *   2) template literals        e.g. `已删除 ${n} 个对话` → "已删除 {var0} 个对话"
 *   3) JSX text nodes           e.g. <span>故障转移</span>
 * Comments (`//`, `/* *\/`) are not AST nodes, so they are excluded automatically. Chinese
 * in leading/trailing comments is not a translation target.
 *
 * Outputs (web/messages/):
 *   - zh.json          namespace-nested { key: "original Chinese" }  (keeps the original for upstream comparison)
 *   - ko.json          same skeleton, values "" (filled with Korean in B3)
 *   - zh.sources.json  per-key source (file:line), kind and placeholders — for B2 wiring, B3 translation and drift tracking
 *
 * Re-extraction is non-destructive. Only the top-level namespaces the extractor produces
 * (path-based: app, components, lib, ...) are refreshed from code; any other "curated
 * namespaces" (hand-translated nav, search, header, ...) are preserved from the existing file.
 * Rule: extracted namespaces are an inventory and are never hand-edited; runtime
 * translations live only in curated namespaces, so re-extraction never overwrites them.
 *
 * Namespaces come from the file path. e.g. src/app/(main)/system/llm/page.tsx
 *   → app.main.system.llm.page  (route-group parentheses and the leading _ of folders removed)
 * Keys are the first 8 chars of the text's sha1. The same text gets the same key even if order
 * or files change, so re-run diffs stay small (helps when comparing upstream updates).
 *
 * Run:  npm run extract:i18n
 */

import * as ts from "typescript";

import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";

const SRC_DIR = path.resolve(__dirname, "..");
const MESSAGES_DIR = path.resolve(__dirname, "../../messages");
const REPO_WEB_DIR = path.resolve(__dirname, "../..");

/** Whether the text contains at least one Han character. The sole criterion for a UI translation target. */
const HAN = /\p{Script=Han}/u;

/** Directories excluded from extraction (the tooling itself and build output). */
const SKIP_DIRS = new Set(["scripts", "node_modules", ".next"]);

type Kind = "string" | "template" | "jsx";

interface Message {
  ns: string;
  key: string;
  text: string;
  kind: Kind;
  placeholders: string[]; // original ${...} expressions of the template (for reference)
  occurrences: string[]; // "relative/path:line"
}

/** Path relative to src → dot-joined namespace. */
function toNamespace(absFile: string): string {
  const rel = path.relative(SRC_DIR, absFile).replace(/\.[tj]sx?$/, "");
  return rel
    .split(path.sep)
    .map((seg) =>
      seg
        .replace(/^\((.*)\)$/, "$1") // (main) → main  (route group)
        .replace(/^_/, "") // _components → components
        .replace(/[^A-Za-z0-9가-힣]+/g, "-")
        .replace(/^-+|-+$/g, ""),
    )
    .filter(Boolean)
    .join(".");
}

function hashKey(text: string): string {
  return crypto.createHash("sha1").update(text).digest("hex").slice(0, 8);
}

/** Collects .ts/.tsx files by walking directories recursively. */
function collectFiles(dir: string, out: string[] = []): string[] {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (SKIP_DIRS.has(entry.name)) continue;
      collectFiles(path.join(dir, entry.name), out);
    } else if (/\.(ts|tsx)$/.test(entry.name)) {
      out.push(path.join(dir, entry.name));
    }
  }
  return out;
}

/** Rebuilds a template literal as "literal parts + {varN}". Returns null when the literal parts contain no Han. */
function reconstructTemplate(node: ts.TemplateExpression): { text: string; placeholders: string[] } | null {
  let text = node.head.text;
  const placeholders: string[] = [];
  node.templateSpans.forEach((span, i) => {
    placeholders.push(span.expression.getText());
    text += `{var${i}}${span.literal.text}`;
  });
  // If Han appears only inside ${} and not in the fixed text, the inner nodes capture it separately.
  if (!HAN.test(node.head.text) && !node.templateSpans.some((s) => HAN.test(s.literal.text))) {
    return null;
  }
  return { text, placeholders };
}

/** Normalizes JSX text: trim both ends and collapse inner whitespace/newlines to one space (same as browser rendering). */
function normalizeJsxText(raw: string): string {
  return raw.replace(/\s+/g, " ").trim();
}

const messages = new Map<string, Message>(); // "ns\u0000key" → Message
const scannedFiles = collectFiles(SRC_DIR).sort();
const capturedLines = new Map<string, Set<number>>(); // file → set of line numbers hit by extracted nodes
let fileWithHan = 0;

function record(ns: string, text: string, kind: Kind, placeholders: string[], relFile: string, line: number) {
  const key = hashKey(text);
  const id = `${ns}\u0000${key}`;
  let msg = messages.get(id);
  if (!msg) {
    msg = { ns, key, text, kind, placeholders, occurrences: [] };
    messages.set(id, msg);
  }
  const occ = `${relFile}:${line}`;
  if (!msg.occurrences.includes(occ)) msg.occurrences.push(occ);
}

for (const absFile of scannedFiles) {
  const source = fs.readFileSync(absFile, "utf8");
  if (!HAN.test(source)) continue;
  fileWithHan++;

  const relFile = path.relative(REPO_WEB_DIR, absFile);
  const ns = toNamespace(absFile);
  const scriptKind = absFile.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const sf = ts.createSourceFile(absFile, source, ts.ScriptTarget.Latest, true, scriptKind);
  const lineOf = (pos: number) => sf.getLineAndCharacterOfPosition(pos).line + 1;
  const markLine = (pos: number) => {
    const set = capturedLines.get(relFile) ?? new Set<number>();
    set.add(lineOf(pos));
    capturedLines.set(relFile, set);
  };

  const visit = (node: ts.Node): void => {
    if (ts.isStringLiteralLike(node) && !ts.isTemplateExpression(node.parent)) {
      // String literals + templates without substitutions. (head/middle of substituted templates do not match here)
      if (HAN.test(node.text)) {
        record(ns, node.text, "string", [], relFile, lineOf(node.getStart(sf)));
        markLine(node.getStart(sf));
      }
    } else if (ts.isTemplateExpression(node)) {
      const r = reconstructTemplate(node);
      if (r) {
        record(ns, r.text, "template", r.placeholders, relFile, lineOf(node.getStart(sf)));
        markLine(node.getStart(sf));
      }
    } else if (ts.isJsxText(node)) {
      const text = normalizeJsxText(node.text);
      if (text && HAN.test(text)) {
        record(ns, text, "jsx", [], relFile, lineOf(node.getStart(sf)));
        markLine(node.getStart(sf));
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
}

// --- Nested JSON assembly ---------------------------------------------------------
type Tree = { [k: string]: Tree | string };

function setNested(root: Tree, nsPath: string, key: string, value: string) {
  const segs = nsPath.split(".");
  let node: Tree = root;
  for (const seg of segs) {
    if (typeof node[seg] !== "object") node[seg] = {};
    node = node[seg] as Tree;
  }
  node[key] = value;
}

/** Sorts keys recursively to minimize re-run diffs. */
function sortTree(t: Tree): Tree {
  const out: Tree = {};
  for (const k of Object.keys(t).sort()) {
    const v = t[k];
    out[k] = typeof v === "string" ? v : sortTree(v);
  }
  return out;
}

const zhTree: Tree = {};
const koTree: Tree = {};
const sources: Record<string, { text: string; kind: Kind; placeholders: string[]; occurrences: string[] }> = {};

for (const msg of messages.values()) {
  setNested(zhTree, msg.ns, msg.key, msg.text);
  setNested(koTree, msg.ns, msg.key, "");
  sources[`${msg.ns}.${msg.key}`] = {
    text: msg.text,
    kind: msg.kind,
    placeholders: msg.placeholders,
    occurrences: msg.occurrences.sort(),
  };
}

const sortedSources: typeof sources = {};
for (const k of Object.keys(sources).sort()) sortedSources[k] = sources[k];

fs.mkdirSync(MESSAGES_DIR, { recursive: true });
const write = (name: string, data: unknown) =>
  fs.writeFileSync(path.join(MESSAGES_DIR, name), `${JSON.stringify(data, null, 2)}\n`, "utf8");

/** Reads an existing message file. Missing or corrupt → empty tree. */
function readTree(name: string): Tree {
  try {
    return JSON.parse(fs.readFileSync(path.join(MESSAGES_DIR, name), "utf8")) as Tree;
  } catch {
    return {};
  }
}

/**
 * Non-destructive merge. Top-level namespaces in the freshly extracted tree (fresh) take
 * code as the source of truth and are used as-is; top-level namespaces found only in the
 * existing file (= hand-translated curated namespaces) are preserved. Stale keys inside
 * extracted namespaces (gone upstream) drop out naturally (helps upstream comparison).
 */
function mergeCurated(fresh: Tree, existing: Tree): Tree {
  const extractedTop = new Set(Object.keys(fresh));
  const out: Tree = { ...fresh };
  for (const [ns, subtree] of Object.entries(existing)) {
    if (!extractedTop.has(ns)) out[ns] = subtree;
  }
  return out;
}

write("zh.json", sortTree(mergeCurated(zhTree, readTree("zh.json"))));
write("ko.json", sortTree(mergeCurated(koTree, readTree("ko.json"))));
write("zh.sources.json", sortedSources);

// --- Summary report ------------------------------------------------------------
const byKind: Record<Kind, number> = { string: 0, template: 0, jsx: 0 };
const byNs = new Map<string, number>();
let totalOccurrences = 0;
for (const msg of messages.values()) {
  byKind[msg.kind]++;
  byNs.set(msg.ns, (byNs.get(msg.ns) ?? 0) + 1);
  totalOccurrences += msg.occurrences.length;
}

// Coverage estimate: share of Han-containing source lines hit by an extracted node.
// The rest are almost all comments (not translation targets).
let hanLines = 0;
let capturedHanLines = 0;
for (const absFile of scannedFiles) {
  const relFile = path.relative(REPO_WEB_DIR, absFile);
  const lines = fs.readFileSync(absFile, "utf8").split(/\r?\n/);
  const capSet = capturedLines.get(relFile) ?? new Set<number>();
  lines.forEach((ln, i) => {
    if (HAN.test(ln)) {
      hanLines++;
      if (capSet.has(i + 1)) capturedHanLines++;
    }
  });
}

const topNs = [...byNs.entries()].sort((a, b) => b[1] - a[1]).slice(0, 15);

console.log("─".repeat(64));
console.log("ARTEX i18n extraction complete");
console.log("─".repeat(64));
console.log(`Files scanned           : ${scannedFiles.length} (.ts/.tsx under src/, excluding scripts)`);
console.log(`Files containing Han    : ${fileWithHan}`);
console.log(`Unique messages (keys) : ${messages.size}`);
console.log(`  ├─ string literals    : ${byKind.string}`);
console.log(`  ├─ template literals  : ${byKind.template}`);
console.log(`  └─ JSX text           : ${byKind.jsx}`);
console.log(`Total occurrences      : ${totalOccurrences} (including repeats)`);
console.log(`Namespaces             : ${byNs.size}`);
console.log("");
console.log(`Source lines with Han  : ${hanLines}`);
console.log(`  └─ captured by extraction: ${capturedHanLines} (${((capturedHanLines / hanLines) * 100).toFixed(1)}%)`);
console.log(
  `     the remaining ${hanLines - capturedHanLines} lines are mostly code comments (not translation targets)`,
);
console.log("");
console.log("Top 15 namespaces by message count:");
for (const [ns, n] of topNs) console.log(`  ${String(n).padStart(4)}  ${ns}`);
console.log("");
console.log(`Output: web/messages/{zh.json, ko.json, zh.sources.json}`);
console.log("─".repeat(64));
