import { useCallback, useEffect, useMemo, useState } from "react";

import { useTranslations } from "next-intl";

import { api } from "@/lib/api";
import type { Agent } from "@/lib/types";

// Names, descriptions and prompt-variable labels of the built-in agents are DB seed data
// in a single source language (builtinAgents in db/db.go, the retester agent in
// server/finding_retests.go). DB values cannot exist per language, and some seeds use
// ON CONFLICT DO NOTHING so older installs keep their original (Chinese) text. The UI
// therefore looks up a translation by agent key in the message files (the builtinAgents
// namespace of web/messages/{ko,en,zh,es,ja}.json) and falls back to the DB value.
// User-created agents always show their DB values.

// retester is seeded with builtin=false (server FindingRetestAgentKey), but it is a fixed
// key planted by the system, so it is translated too. Agents with builtin=true (goals/
// planner/mainagent/worker/auto/pentest) are caught by agent.builtin and need no entry.
const SYSTEM_SEEDED_AGENT_KEYS = new Set(["retester"]);

function isSystemSeeded(agent: { key: string; builtin?: boolean }): boolean {
  return !!agent.builtin || SYSTEM_SEEDED_AGENT_KEYS.has(agent.key);
}

// reporter is planted by the system but editable in the UI (seedReporterAgent in
// server/orchestration.go), so it is translated only while the DB value still equals a
// known seed text. Values edited by the user are shown as is. Each field lists every
// seed text that has shipped: the current one first, then legacy ones.
const EDITABLE_SEEDS: Record<string, { names: string[]; descriptions: string[] }> = {
  reporter: {
    names: [
      "Report writer",
      // legacy: Korean seed name, still stored on installs seeded before the English switch.
      "보고서 작성",
    ],
    descriptions: [
      "Writes detailed vulnerability reports: triggered automatically when a vulnerability is found, it reviews the evidence and execution process, then writes a Markdown report and records it on that vulnerability.",
      // legacy: Korean seed description, still stored on installs seeded before the English switch.
      "취약점 상세 보고서 작성: 취약점을 발견하면 자동으로 트리거되어, 증거와 실행 과정을 조회한 뒤 Markdown 보고서를 작성해 해당 취약점에 기록합니다.",
    ],
  },
};

// useAgentLabels returns helpers that give built-in agents' names, descriptions and
// prompt-variable labels in the current language. Every display site goes through them.
export function useAgentLabels() {
  const t = useTranslations("builtinAgents");

  // Callers use these functions as useMemo/useCallback dependencies (conversation
  // grouping, filters), so a new bundle is built only when the language (t) changes.
  // A new object on every render would rerun those computations every time.
  return useMemo(() => {
    // name returns the translated name for built-in agents, otherwise the DB name (agent.name).
    const name = (agent: { key: string; name: string; builtin?: boolean }): string => {
      const translatable = isSystemSeeded(agent) || !!EDITABLE_SEEDS[agent.key]?.names.includes(agent.name);
      if (translatable && t.has(`${agent.key}.name`)) return t(`${agent.key}.name`);
      return agent.name;
    };

    // description returns the translated description for built-in agents, otherwise the
    // DB description (or an empty string). Callers add the empty-description fallback text.
    const description = (agent: { key: string; description?: string; builtin?: boolean }): string => {
      const translatable =
        isSystemSeeded(agent) ||
        (agent.description !== undefined && !!EDITABLE_SEEDS[agent.key]?.descriptions.includes(agent.description));
      if (translatable && t.has(`${agent.key}.description`)) return t(`${agent.key}.description`);
      return agent.description ?? "";
    };

    // nameByKey is the variant for when only a key is at hand (e.g. grouping conversations).
    // It takes a lookup function returning the agent: translated name if found, else the key.
    const nameByKey = (
      key: string,
      lookup: (key: string) => { key: string; name: string; builtin?: boolean } | undefined,
    ): string => {
      const agent = lookup(key);
      // biome-ignore lint/nursery/useNullishCoalescing: an agent with an empty ("") name must still show its key instead, so || is intended.
      return (agent && name(agent)) || key;
    };

    // varLabel translates a built-in agent's prompt-variable label (e.g. goals'
    // "Overall task goal"). Without a translation it uses the DB label (fallback).
    const varLabel = (agentKey: string, builtin: boolean, varName: string, fallback: string): string => {
      if ((builtin || SYSTEM_SEEDED_AGENT_KEYS.has(agentKey)) && t.has(`${agentKey}.vars.${varName}`)) {
        return t(`${agentKey}.vars.${varName}`);
      }
      return fallback;
    };

    return { name, description, nameByKey, varLabel };
  }, [t]);
}

// Several screens (approval records, etc.) use the agent list only to map key → display
// name, so it is fetched once and shared at module level. On failure the next call refetches.
let agentListPromise: Promise<Agent[]> | null = null;
function loadAgentList(): Promise<Agent[]> {
  if (!agentListPromise) {
    agentListPromise = api.agents().catch((err) => {
      agentListPromise = null;
      throw err;
    });
  }
  return agentListPromise;
}

// useAgentNameByKey returns a converter for screens that only have a key, not an agent
// object (e.g. conv_agent_key in approval/intercept records). Built-in agents get their
// name in the current language, user agents their DB name, and unknown or not-yet-loaded
// keys are shown as is.
export function useAgentNameByKey(): (key: string) => string {
  const labels = useAgentLabels();
  const [agents, setAgents] = useState<Map<string, Agent>>(() => new Map());
  useEffect(() => {
    let alive = true;
    loadAgentList()
      .then((list) => {
        if (alive) setAgents(new Map(list.map((a) => [a.key, a])));
      })
      .catch(() => {
        // If the list cannot be fetched, keys are shown as is (retried on the next mount).
      });
    return () => {
      alive = false;
    };
  }, []);
  return useCallback((key: string) => labels.nameByKey(key, (k) => agents.get(k)), [labels, agents]);
}
