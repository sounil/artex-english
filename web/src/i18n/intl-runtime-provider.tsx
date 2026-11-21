"use client";

import type { ReactNode } from "react";
import * as React from "react";

import { NextIntlClientProvider } from "next-intl";

import enMessages from "../../messages/en.json";
import esMessages from "../../messages/es.json";
import jaMessages from "../../messages/ja.json";
import koMessages from "../../messages/ko.json";
import zhMessages from "../../messages/zh.json";
import type { Locale } from "./config";
import { useActiveLocale } from "./locale-store";

// Messages for all five languages are bundled statically. That way, when the user switches language in settings,
// every string re-renders in the chosen language on the spot, with no rebuild, page reload or async loading
// (also compatible with static export output: "export").
const MESSAGES: Record<Locale, Record<string, unknown>> = {
  en: enMessages as Record<string, unknown>,
  ko: koMessages as Record<string, unknown>,
  zh: zhMessages as Record<string, unknown>,
  es: esMessages as Record<string, unknown>,
  ja: jaMessages as Record<string, unknown>,
};

// Time zone of the runtime environment (browser or prerender server).
const TIME_ZONE = Intl.DateTimeFormat().resolvedOptions().timeZone;

// Provider for runtime language switching. Instead of the root layout's NextIntlClientProvider,
// it picks and supplies messages for the active locale the user chose (cookie/localStorage).
export function IntlRuntimeProvider({ children }: Readonly<{ children: ReactNode }>) {
  const locale = useActiveLocale();

  // When the language changes, update <html lang> too. This overrides the initial lang set by the root layout on
  // the server with the client's choice, so screen readers and browser translation use the right language.
  React.useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  // Unlike the root layout's provider, this does not inherit the server request config, so timeZone is
  // passed directly. Without it next-intl logs an ENVIRONMENT_FALLBACK error during prerender.
  return (
    <NextIntlClientProvider locale={locale} messages={MESSAGES[locale]} timeZone={TIME_ZONE}>
      {children}
    </NextIntlClientProvider>
  );
}
