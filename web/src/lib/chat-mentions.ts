// label is the category written into tokens (@[finding#12 name]); it must match
// chatMentionPattern in server/chat_mentions.go. legacy is the Chinese label used by
// tokens written before the English switch; it is still parsed, and still accepted
// as a typed alias.
export const mentionKinds = [
  { kind: "finding", label: "finding", alias: "finding", legacy: "漏洞" },
  { kind: "asset", label: "asset", alias: "asset", legacy: "资产" },
  { kind: "company", label: "company", alias: "company", legacy: "企业" },
  { kind: "endpoint", label: "endpoint", alias: "api", legacy: "接口" },
  { kind: "ip", label: "IP", alias: "ip", legacy: "IP" },
  { kind: "app", label: "app", alias: "app", legacy: "应用" },
  { kind: "root_domain", label: "domain", alias: "domain", legacy: "域名" },
  { kind: "subdomain", label: "subdomain", alias: "subdomain", legacy: "子域名" },
  { kind: "service", label: "service", alias: "service", legacy: "服务" },
] as const;

export type MentionKind = (typeof mentionKinds)[number]["kind"];
export interface ChatMention {
  kind: MentionKind;
  id: number;
  label: string;
  description: string;
}

export function activeMention(value: string, caret: number) {
  const before = value.slice(0, caret);
  const start = before.lastIndexOf("@");
  if (start < 0 || (start > 0 && /[\w.+/-]/.test(before[start - 1]))) return null;
  const query = before.slice(start + 1);
  if (/[[\]\r\n@]/.test(query) || query.length > 220) return null;
  return { start, end: caret, query };
}

export function mentionSearch(query: string) {
  const text = query.trimStart().toLowerCase();
  for (const item of mentionKinds) {
    for (const alias of [item.label.toLowerCase(), item.alias, item.legacy.toLowerCase()]) {
      if (text === alias || text.startsWith(`${alias} `) || (/[^a-z]/.test(alias) && text.startsWith(alias))) {
        return { kind: item.kind, query: query.trimStart().slice(alias.length).trim(), categories: [] };
      }
    }
  }
  const categories = mentionKinds.filter(
    (item) =>
      item.label.toLowerCase().startsWith(text) ||
      item.alias.startsWith(text) ||
      item.legacy.toLowerCase().startsWith(text),
  );
  return { kind: "" as const, query: query.trim(), categories };
}

export function mentionToken(item: ChatMention) {
  const kind = mentionKinds.find((entry) => entry.kind === item.kind)?.label ?? "asset";
  const label = item.label
    .replace(/[[\]]/g, (char) => (char === "[" ? "(" : ")"))
    .replace(/\s+/g, " ")
    .slice(0, 100);
  return `@[${kind}#${item.id} ${label}]`;
}

export interface SelectedMention {
  token: string;
  kind: MentionKind;
  id: string;
  text: string;
  start: number;
}

const tokenLabels = mentionKinds.flatMap((entry) =>
  entry.legacy === entry.label ? [entry.label] : [entry.label, entry.legacy],
);
const tokenPattern = new RegExp(`@\\[(${tokenLabels.join("|")})#([0-9]+)(?: ([^\\]\\r\\n]*))?\\]`, "g");

// Token categories map back to the kind here (English labels, plus legacy Chinese ones
// from older messages); consuming components localize it via mentionTextarea.kind.
export function selectedMentions(value: string): SelectedMention[] {
  return [...value.matchAll(tokenPattern)].map((match) => ({
    token: match[0],
    kind: mentionKinds.find((entry) => entry.label === match[1] || entry.legacy === match[1])?.kind ?? "asset",
    id: match[2],
    text: match[3] ?? "",
    start: match.index,
  }));
}
