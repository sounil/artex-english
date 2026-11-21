import { DEFAULT_LOCALE, isLocale, LOCALE_STORAGE_KEY, type Locale, resolveLocale } from "@/i18n/config";
import { getLocalStorageValue } from "@/lib/local-storage.client";

// BCP47 locale tag for date/time formatting. It follows the display language (locale-store), so use this
// function instead of a hard-coded "ko-KR". On server render (no window) it uses the build default locale;
// on the client it reflects the user's choice. It is also safe for Intl formatters read once at module
// load time.
const DATE_LOCALE: Record<Locale, string> = {
  en: "en-US",
  ko: "ko-KR",
  zh: "zh-CN",
  es: "es-ES",
  ja: "ja-JP",
};

export function getDateLocale(): string {
  if (typeof window === "undefined") return DATE_LOCALE[DEFAULT_LOCALE];
  const raw = getLocalStorageValue(LOCALE_STORAGE_KEY);
  const loc: Locale = isLocale(raw) ? raw : resolveLocale();
  return DATE_LOCALE[loc];
}
