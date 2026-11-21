import { useMemo } from "react";

import { useTranslations } from "next-intl";

import toolSources from "./builtin-tool-sources.json";

// Built-in intercept rule names, asset rule notes and tool descriptions are DB seed data,
// so they cannot exist per language. They are translated only on screen through the
// message files (builtinRules, builtinAssetNotes, builtinTools in web/messages/*.json);
// when a DB value differs from the seed text (edited by the user, or a description changed
// in code) it is shown as is. A tool description in the DB is guidance sent to the model,
// so the edit form never receives the translated text.
// The source text contains braces, so it is read with t.raw to skip ICU parsing.
//
// Seed names are keyed by their current English seed text (db/db.go). Installs seeded
// before the English switch keep their Chinese rows (seeds use ON CONFLICT DO NOTHING),
// so the old Chinese names stay below as legacy aliases.

const RULE_KEYS: Record<string, string> = {
  "[Built-in] Recursive force delete rm -rf": "rmRf",
  "[Built-in] Delete critical system directories": "rmSystemDirs",
  "[Built-in] Disk format mkfs": "mkfs",
  "[Built-in] Overwrite disk device dd": "ddOverwrite",
  "[Built-in] Fork bomb": "forkBomb",
  "[Built-in] Shutdown / reboot": "shutdownReboot",
  "[Built-in] Kill all processes": "killAll",
  "[Built-in] Disk wipe shred / wipe": "shredWipe",
  "[Built-in] Flush firewall rules": "flushFirewall",
  "[Built-in] SQL DROP DATABASE / TABLE / SCHEMA": "sqlDrop",
  "[Built-in] SQL TRUNCATE": "sqlTruncate",
  "[Built-in] MongoDB drop / dropDatabase": "mongoDrop",
  "[Built-in] Redis FLUSHALL / FLUSHDB": "redisFlush",
  "[Built-in] curl / wget DELETE request": "curlWgetDelete",
  "[Built-in] Python HTTP client DELETE (requests/httpx/aiohttp)": "pythonHttpDelete",
  "[Built-in] HTTP DELETE method in scripts (JS/generic)": "scriptHttpDelete",
  "[Built-in] Bulk clear / purge endpoint path": "bulkClearPath",
  "[Built-in] Destructive system command": "destructiveCommand",
  "[Built-in] Data exfiltration pipe": "exfilPipe",
  "[Built-in] Delete endpoint path": "deletePath",
  // legacy: Chinese seed names still stored on installs seeded before the English switch.
  "[内置] 递归强制删除 rm -rf": "rmRf",
  "[内置] 删除系统关键目录": "rmSystemDirs",
  "[内置] 磁盘格式化 mkfs": "mkfs",
  "[内置] 覆写磁盘设备 dd": "ddOverwrite",
  "[内置] Fork 炸弹": "forkBomb",
  "[内置] 关机 / 重启": "shutdownReboot",
  "[内置] 杀死全部进程": "killAll",
  "[内置] 磁盘擦除 shred / wipe": "shredWipe",
  "[内置] 清空防火墙规则": "flushFirewall",
  "[内置] SQL DROP DATABASE / TABLE / SCHEMA": "sqlDrop",
  "[内置] SQL TRUNCATE": "sqlTruncate",
  "[内置] MongoDB drop / dropDatabase": "mongoDrop",
  "[内置] Redis FLUSHALL / FLUSHDB": "redisFlush",
  "[内置] curl / wget 发送 DELETE 请求": "curlWgetDelete",
  "[内置] Python HTTP 客户端 DELETE（requests/httpx/aiohttp）": "pythonHttpDelete",
  "[内置] 脚本中声明 HTTP DELETE 方法（JS/通用）": "scriptHttpDelete",
  "[内置] 批量清空 / 清除接口路径": "bulkClearPath",
  "[内置] 破坏性系统命令": "destructiveCommand",
  "[内置] 数据外泄管道": "exfilPipe",
  "[内置] 删除类接口路径": "deletePath",
};

const ASSET_NOTE_KEYS: Record<string, string> = {
  "[Built-in] Government sites (.gov)": "govGlobal",
  "[Built-in] Government sites (.gov.cn)": "govCn",
  "[Built-in] Education sites (.edu)": "eduGlobal",
  "[Built-in] Education sites (.edu.cn)": "eduCn",
  // legacy: Chinese seed names still stored on installs seeded before the English switch.
  "[内置] 政府网站 (.gov)": "govGlobal",
  "[内置] 政府网站 (.gov.cn)": "govCn",
  "[内置] 教育网站 (.edu)": "eduGlobal",
  "[内置] 教育网站 (.edu.cn)": "eduCn",
};

const TOOL_SOURCES: Record<string, string> = toolSources;

// Section headings prescribed by the reporter agent prompt (agent/promptcatalog.go).
// Stored reports are not rewritten; headings are only replaced for display. Copy and
// export keep the original text. PoC is not translated.
const REPORT_HEADING_KEYS: Record<string, string> = {
  Overview: "overview",
  Impact: "impact",
  "Affected Scope": "scope",
  "Steps to Reproduce": "reproduce",
  Evidence: "evidence",
  "Root Cause Analysis": "rootCause",
  Remediation: "remediation",
  // legacy: Chinese headings from the original prompt, still present in stored reports.
  概述: "overview",
  影响与危害: "impact",
  受影响范围: "scope",
  复现步骤: "reproduce",
  证据: "evidence",
  根因分析: "rootCause",
  修复建议: "remediation",
};

export function useBuiltinLabels() {
  const tRule = useTranslations("builtinRules");
  const tNote = useTranslations("builtinAssetNotes");
  const tTool = useTranslations("builtinTools");
  const tHeading = useTranslations("builtinReportHeadings");

  return useMemo(() => {
    const ruleName = (name: string): string => {
      const key = RULE_KEYS[name];
      return key && tRule.has(key) ? (tRule.raw(key) as string) : name;
    };

    const assetNote = (note: string): string => {
      const key = ASSET_NOTE_KEYS[note];
      return key && tNote.has(key) ? (tNote.raw(key) as string) : note;
    };

    const toolDescription = (tool: { key: string; system: boolean; description: string }): string => {
      if (tool.system && TOOL_SOURCES[tool.key] === tool.description && tTool.has(tool.key)) {
        return tTool.raw(tool.key) as string;
      }
      return tool.description;
    };

    // Lines inside code fences are raw evidence and are left untouched.
    const reportHeadings = (markdown: string): string => {
      let inFence = false;
      return markdown
        .split("\n")
        .map((line) => {
          if (/^\s*(```|~~~)/.test(line)) inFence = !inFence;
          if (inFence) return line;
          const match = /^(#{1,6}\s+)(.+?)\s*$/.exec(line);
          const key = match && REPORT_HEADING_KEYS[match[2]];
          return match && key ? match[1] + (tHeading.raw(key) as string) : line;
        })
        .join("\n");
    };

    return { ruleName, assetNote, toolDescription, reportHeadings };
  }, [tRule, tNote, tTool, tHeading]);
}
