import { DEFAULT_LOCALE, type Locale, resolveLocale } from "@/i18n/config";

import packageJson from "../../package.json";

const currentYear = new Date().getFullYear();

// Browser tab title and search-engine meta description. These go into the static metadata in layout.tsx and
// render into every page's <title> and <meta name="description">, so they branch across the five languages by the
// build default locale. zh keeps the upstream original text for comparison. Titles use a colon instead of an em dash,
// as "Product: description". This metadata is a static build-time value and does not react to runtime language
// switching (acceptable for a one-line tab title).
const META_BY_LOCALE: Record<Locale, { title: string; description: string }> = {
  en: {
    title: "ARTEX: Autonomous Penetration Testing Console",
    description: "An LLM-driven system console for autonomous penetration testing",
  },
  ko: {
    title: "ARTEX: 자율 침투 테스트 콘솔",
    description: "LLM 기반으로 자율 침투 테스트를 수행하는 시스템 콘솔",
  },
  zh: {
    title: "ARTEX — 自主渗透测试控制台",
    description: "LLM 驱动的自主渗透测试系统控制台",
  },
  es: {
    title: "ARTEX: Consola de Pruebas de Penetración Autónomas",
    description: "Una consola de sistema impulsada por LLM para pruebas de penetración autónomas",
  },
  ja: {
    title: "ARTEX: 自律型ペネトレーションテストコンソール",
    description: "LLM を活用して自律的にペネトレーションテストを行うシステムコンソール",
  },
};

export const APP_CONFIG = {
  name: "ARTEX",
  version: packageJson.version,
  copyright: `© ${currentYear}, ARTEX.`,
  meta: META_BY_LOCALE[resolveLocale()] ?? META_BY_LOCALE[DEFAULT_LOCALE],
};
