import { activeMention, mentionSearch, mentionToken, selectedMentions } from "./chat-mentions.ts";
import assert from "node:assert/strict";
import test from "node:test";

test("mention trigger handles CJK text and cursor placement without hijacking email", () => {
  assert.equal(activeMention("user@example.com", 16), null);
  assert.equal(activeMention("picked @[finding#1 X]", 21), null);
  assert.deepEqual(activeMention("see@finding text", 11), null);
  assert.deepEqual(activeMention("see @finding text", 12), { start: 4, end: 12, query: "finding" });
  // CJK text right before "@" is not a word character, so the trigger still fires.
  assert.deepEqual(activeMention("查看@漏洞 后面的文字", 5), { start: 2, end: 5, query: "漏洞" });
  assert.equal(activeMention("@finding\nnext line", 15), null);
});

test("categories, English and legacy Chinese aliases, IP and keyword search", () => {
  assert.equal(mentionSearch("").categories.length, 9);
  assert.equal(mentionSearch("fin").categories[0].kind, "finding");
  assert.equal(mentionSearch("finding").kind, "finding");
  assert.equal(mentionSearch("finding SQL injection").query, "SQL injection");
  assert.equal(mentionSearch("ip 192.0.2.1").kind, "ip");
  assert.equal(mentionSearch("api GET /api").query, "GET /api");
  assert.equal(mentionSearch("acme.com").kind, "");
  // legacy: Chinese category names typed without a space still work.
  assert.equal(mentionSearch("漏").categories[0].kind, "finding");
  assert.equal(mentionSearch("漏洞SQL注入").kind, "finding");
  assert.equal(mentionSearch("漏洞SQL注入").query, "SQL注入");
});

test("tokens roundtrip labels and removing one reference preserves its neighbors", () => {
  const first = mentionToken({ kind: "finding", id: 12, label: "Title[1]\nDescription" });
  const second = mentionToken({ kind: "ip", id: 13, label: "192.0.2.1" });
  assert.equal(first, "@[finding#12 Title(1) Description]");
  const value = `analyze ${first} and ${second}`;
  const selected = selectedMentions(value);
  assert.equal(selected.length, 2);
  assert.equal(selected[0].kind, "finding");
  assert.equal(selected[0].id, "12");
  assert.equal(selected[0].text, "Title(1) Description");
  assert.equal(selected[1].kind, "ip");
  const next = value.slice(0, selected[0].start) + value.slice(selected[0].start + selected[0].token.length);
  assert.equal(selectedMentions(next)[0].token, second);
});

test("legacy Chinese tokens from older messages still parse", () => {
  const selected = selectedMentions("分析 @[漏洞#12 标题] 和 @[子域名#3 a.example.com] @[域名#4]");
  assert.deepEqual(
    selected.map((m) => [m.kind, m.id]),
    [
      ["finding", "12"],
      ["subdomain", "3"],
      ["root_domain", "4"],
    ],
  );
});
