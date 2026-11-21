import { describe, expect, it } from "vitest";

import { DEFAULT_LOCALE, resolveLocale } from "./config";

describe("i18n config", () => {
  it("is English only", () => {
    expect(DEFAULT_LOCALE).toBe("en");
    expect(resolveLocale()).toBe("en");
  });
});
