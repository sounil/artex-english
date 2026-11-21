"use client";

import * as React from "react";

import { useTranslations } from "next-intl";

import { getLocalStorageValue, setLocalStorageValue } from "@/lib/local-storage.client";

// Send/newline key combo for the session input. A pure front-end preference, stored only in localStorage,
// not on the server and not synced with the account, so it must be set again in another browser.
// See issue #39: 0.3.2 changed Ctrl+Enter-to-send into Enter-to-send, and this brings the old
// combo back as an option.
export type ChatSendMode = "enter" | "ctrl-enter";

export const CHAT_SEND_MODE_KEY = "artex_chat_send_mode";
export const DEFAULT_CHAT_SEND_MODE: ChatSendMode = "enter";

// Options for the send-mode dropdown in settings. Labels depend on the locale, so they are returned from a hook
// rather than a module constant (hard-coding one language in a pure module would break the other locales).
// value is a stable identifier used as is for the localStorage key and behavior checks.
export function useChatSendModeOptions(): { value: ChatSendMode; label: string }[] {
  const t = useTranslations("chatSendMode.option");
  return [
    { value: "enter", label: t("enter") },
    { value: "ctrl-enter", label: t("ctrlEnter") },
  ];
}

function parseMode(raw: string | null): ChatSendMode {
  return raw === "ctrl-enter" || raw === "enter" ? raw : DEFAULT_CHAT_SEND_MODE;
}

// Subscribers within the same tab. localStorage's storage event only fires in "other" tabs, so after
// changing the setting in the current tab, emit must notify same-tab inputs. Otherwise it only takes
// effect after a reload.
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  window.addEventListener("storage", listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("storage", listener);
  };
}

// Returns string literals, so Object.is compares by value and useSyncExternalStore does not fall into an
// infinite loop.
function getSnapshot(): ChatSendMode {
  return parseMode(getLocalStorageValue(CHAT_SEND_MODE_KEY));
}

// The server has no localStorage, so the default renders first, and after hydration getSnapshot
// corrects the value.
function getServerSnapshot(): ChatSendMode {
  return DEFAULT_CHAT_SEND_MODE;
}

export function useChatSendMode(): ChatSendMode {
  return React.useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

export function setChatSendMode(mode: ChatSendMode) {
  setLocalStorageValue(CHAT_SEND_MODE_KEY, mode);
  for (const listener of listeners) listener();
}

// shouldSubmitOnKey decides whether a single keystroke means "send".
// isComposing / keyCode 229 mean an IME (Korean, Chinese, etc.) is composing characters, so they
// must always pass through. Otherwise pressing Enter to commit a character would send by mistake.
// enter mode excludes only Shift, keeping 0.3.2's behavior exactly (users who never changed
// the setting feel no difference). ctrl-enter mode accepts both Ctrl and Cmd (macOS)
// together.
export function shouldSubmitOnKey(e: React.KeyboardEvent, mode: ChatSendMode): boolean {
  if (e.key !== "Enter") return false;
  if (e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229) return false;
  if (mode === "ctrl-enter") return e.ctrlKey || e.metaKey;
  return !e.shiftKey;
}
