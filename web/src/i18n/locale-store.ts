"use client";

import * as React from "react";

import { setClientCookie } from "@/lib/cookie.client";
import { getLocalStorageValue, setLocalStorageValue } from "@/lib/local-storage.client";

import { isLocale, LOCALE_COOKIE, LOCALE_STORAGE_KEY, type Locale, resolveLocale } from "./config";

// The user's chosen display language is a pure front-end preference, so it is not stored on the server,
// only in localStorage (and a cookie). It is not synced with the account, so switching browsers or clearing
// site data reverts to the default language. The agent output language is stored separately in server settings.

function parseLocale(raw: string | null): Locale {
  return isLocale(raw) ? raw : resolveLocale();
}

// Subscribers within the same tab. The storage event only fires in "other" tabs, so after changing the
// language in the current tab, emit must notify same-tab subscribers for an immediate re-render.
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  window.addEventListener("storage", listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("storage", listener);
  };
}

function emit() {
  for (const listener of listeners) listener();
}

function getSnapshot(): Locale {
  return parseLocale(getLocalStorageValue(LOCALE_STORAGE_KEY));
}

// The server has no localStorage, so the build default locale renders first, and after hydration
// getSnapshot corrects it to the user's choice.
function getServerSnapshot(): Locale {
  return resolveLocale();
}

// Hook subscribing to the active display language. Components using it re-render when the language changes.
export function useActiveLocale(): Locale {
  return React.useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

// Changes the display language: writes the cookie and localStorage and notifies same-tab subscribers. Kept for
// one year so the choice survives the next visit.
export function setActiveLocale(locale: Locale) {
  if (!isLocale(locale)) return;
  setLocalStorageValue(LOCALE_STORAGE_KEY, locale);
  setClientCookie(LOCALE_COOKIE, locale, 365);
  emit();
}
