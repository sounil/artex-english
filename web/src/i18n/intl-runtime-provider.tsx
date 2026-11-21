"use client";

import type { ReactNode } from "react";

import { NextIntlClientProvider } from "next-intl";

import enMessages from "../../messages/en.json";
import { DEFAULT_LOCALE } from "./config";

// Time zone of the runtime environment (browser or prerender server).
const TIME_ZONE = Intl.DateTimeFormat().resolvedOptions().timeZone;

// Supplies the English string catalog to client components. timeZone is passed explicitly; without it
// next-intl logs an ENVIRONMENT_FALLBACK error during prerender.
export function IntlRuntimeProvider({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <NextIntlClientProvider
      locale={DEFAULT_LOCALE}
      messages={enMessages as Record<string, unknown>}
      timeZone={TIME_ZONE}
    >
      {children}
    </NextIntlClientProvider>
  );
}
