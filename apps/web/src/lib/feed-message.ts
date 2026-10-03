/** Resolves a step's `message_key` to a translation key under `P1Agents.feed`, never rendering a raw key. */

const NS = "P1Agents.feed.";
const FALLBACK = `${NS}fallback`;
const SAFE_KEY = /^[a-z0-9_]+(\.[a-z0-9_]+){0,5}$/;

export type FeedMessage = { key: string; args: Record<string, string> };

/**
 * `has` answers for full keys (`P1Agents.feed.<key>`). A known key is used as is; an unknown or malformed one (a
 * future Track C/D key, or junk) becomes the generic fallback with the original key folded into the args.
 */
export function feedMessage(has: (key: string) => boolean, key: string, args: Record<string, string>): FeedMessage {
  const full = `${NS}${key}`;
  if (SAFE_KEY.test(key) && key !== "fallback" && has(full)) return { key: full, args };
  return { key: FALLBACK, args: { ...args, key } };
}

type Translator = ((key: string, args?: Record<string, string>) => string) & { has: (key: string) => boolean };

/** `feedMessage` + render. A key that resolves to a namespace object makes `t` throw; that also falls back. */
export function translateFeed(t: Translator, key: string, args: Record<string, string>): string {
  const m = feedMessage((k) => t.has(k), key, args);
  try {
    return t(m.key, m.args);
  } catch {
    return t(FALLBACK, { ...args, key });
  }
}
