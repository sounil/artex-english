// Supported locales and the default. artex-global defaults to English and adds Korean, Chinese, Spanish and Japanese,
// so five languages can be switched in user settings. Chinese ("zh") is also kept for comparison with the upstream source text.
export const LOCALES = ["en", "ko", "zh", "es", "ja"] as const;
export type Locale = (typeof LOCALES)[number];

// The default display language is English; Korean, Chinese, Spanish and Japanese are switched to in settings. It can be
// overridden at build time with NEXT_PUBLIC_LOCALE, and at runtime the value the user picked in settings (cookie/localStorage) has the final say.
export const DEFAULT_LOCALE: Locale = "en";

// Each locale's autonym shown in the language picker. Whatever language you are viewing in, a language's own
// name should read in that language, so these are fixed spellings, not translated.
export const LOCALE_LABELS: Record<Locale, string> = {
  en: "English",
  ko: "한국어",
  zh: "中文",
  es: "Español",
  ja: "日本語",
};

// Key storing the user's chosen display language. The UI reads the localStorage value. The same value is also
// written to a cookie, but nothing reads it yet (kept for when the server needs to detect the language).
export const LOCALE_COOKIE = "artex_locale";
export const LOCALE_STORAGE_KEY = "artex_locale";

// Guard that narrows an arbitrary string to a supported locale.
export function isLocale(raw: unknown): raw is Locale {
  return typeof raw === "string" && (LOCALES as readonly string[]).includes(raw);
}

// Decides the active locale at build time. It must be compatible with static export (output: "export" in next.config),
// so it reads only env vars and does not use dynamic APIs like cookies() or headers().
// An empty or unsupported NEXT_PUBLIC_LOCALE falls back to the default. This value is only used for the
// server prerender's initial HTML; the actual display language is overridden on the client from the cookie.
export function resolveLocale(): Locale {
  const raw = process.env.NEXT_PUBLIC_LOCALE;
  return isLocale(raw) ? raw : DEFAULT_LOCALE;
}
