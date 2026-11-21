import { getRequestConfig } from "next-intl/server";

import { type Locale, resolveLocale } from "./config";

// Per-locale message loaders. An explicit import map keeps the bundler from pulling the whole messages/
// directory in as a context module (excluding the local-only zh.sources.json).
const loaders: Record<Locale, () => Promise<{ default: Record<string, unknown> }>> = {
  en: () => import("../../messages/en.json"),
  ko: () => import("../../messages/ko.json"),
  zh: () => import("../../messages/zh.json"),
  es: () => import("../../messages/es.json"),
  ja: () => import("../../messages/ja.json"),
};

// next-intl request config. This setup does not use i18n path routing (segments/middleware), so the
// locale is decided here directly. requestLocale is not referenced, so it does not trigger dynamic
// rendering. This config only sets the default language for server components and prerender; runtime
// switching by the user is handled by the client-side IntlRuntimeProvider (messages/en·ko·zh·es·ja).
export default getRequestConfig(async () => {
  const locale = resolveLocale();
  const messages = (await loaders[locale]()).default;
  return { locale, messages };
});
