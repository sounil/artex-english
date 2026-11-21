// The UI is English only. next-intl is kept as the string catalog (messages/en.json) so components
// look text up by key; there is no language switching.
export const DEFAULT_LOCALE = "en" as const;
export type Locale = typeof DEFAULT_LOCALE;

// Returns the UI locale (always English). Kept as a function so callers need not change.
export function resolveLocale(): Locale {
  return DEFAULT_LOCALE;
}
