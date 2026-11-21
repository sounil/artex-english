import { getRequestConfig } from "next-intl/server";

import { resolveLocale } from "./config";

// next-intl request config for server components and prerender. The UI is English only, so this
// always loads messages/en.json. No path routing or middleware is used.
export default getRequestConfig(async () => {
  const locale = resolveLocale();
  const messages = (await import("../../messages/en.json")).default as Record<string, unknown>;
  return { locale, messages };
});
