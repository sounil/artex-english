import { NextIntlClientProvider } from "next-intl";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import en from "../../messages/en.json";
import { useAgentLabels } from "./agent-i18n";
import { useBuiltinLabels } from "./builtin-i18n";
import toolSources from "./builtin-tool-sources.json";

function labelsOf<T>(useLabels: () => T): T {
  let out: T | undefined;
  const Probe = () => {
    out = useLabels();
    return null;
  };
  renderToStaticMarkup(
    <NextIntlClientProvider locale="en" messages={en}>
      <Probe />
    </NextIntlClientProvider>,
  );
  return out as T;
}

describe("useBuiltinLabels", () => {
  const labels = labelsOf(useBuiltinLabels);

  it("translates rule names and notes when they are the seed source text", () => {
    expect(labels.ruleName("[内置] 磁盘格式化 mkfs")).toBe(en.builtinRules.mkfs);
    expect(labels.assetNote("[内置] 政府网站 (.gov)")).toBe(en.builtinAssetNotes.govGlobal);
  });

  it("leaves user-edited values as is", () => {
    expect(labels.ruleName("My rule")).toBe("My rule");
    expect(labels.assetNote("My note")).toBe("My note");
    expect(labels.toolDescription({ key: "add_hint", system: true, description: "Edited" })).toBe("Edited");
  });

  it("translates only report subheadings and leaves the body and code blocks as is", () => {
    const report = "## 概述\n概述 body\n\n## PoC\n```text\n## 证据\n```\n\n## 修复建议\nfix";
    expect(labels.reportHeadings(report)).toBe(
      `## ${en.builtinReportHeadings.overview}\n概述 body\n\n## PoC\n\`\`\`text\n## 证据\n\`\`\`\n\n## ${en.builtinReportHeadings.remediation}\nfix`,
    );
  });

  it("does not translate custom tools", () => {
    const description = toolSources.add_hint;
    expect(labels.toolDescription({ key: "add_hint", system: false, description })).toBe(description);
  });

  it("translates every system tool description without failing on braces", () => {
    for (const [key, description] of Object.entries(toolSources)) {
      const translated = labels.toolDescription({ key, system: true, description });
      expect(translated).toBe(en.builtinTools[key as keyof typeof en.builtinTools]);
    }
  });
});

describe("useAgentLabels reporter", () => {
  const labels = labelsOf(useAgentLabels);

  it("looks up translations only for seed values", () => {
    expect(labels.name({ key: "reporter", name: "보고서 작성" })).toBe(en.builtinAgents.reporter.name);
    expect(labels.name({ key: "reporter", name: "My report" })).toBe("My report");
    expect(labels.description({ key: "reporter", description: "My description" })).toBe("My description");
  });
});
