import { describe, expect, it } from "vitest";
import { feedMessage, translateFeed } from "./feed-message";

const has = (known: string[]) => (k: string) => known.includes(k);

describe("feedMessage", () => {
  it("returns the namespaced key unchanged for a known key", () => {
    const out = feedMessage(has(["P1Agents.feed.intake.classified"]), "intake.classified", { kind: "invoice" });
    expect(out).toEqual({ key: "P1Agents.feed.intake.classified", args: { kind: "invoice" } });
  });

  it("falls back for an unknown key and folds the original key into the args", () => {
    const out = feedMessage(has([]), "future.thing", { a: "1" });
    expect(out).toEqual({ key: "P1Agents.feed.fallback", args: { a: "1", key: "future.thing" } });
  });

  it("the original key wins over a server-supplied `key` arg", () => {
    expect(feedMessage(has([]), "k.x", { key: "spoof" }).args.key).toBe("k.x");
  });

  it("treats empty or malformed keys as unknown (never looked up)", () => {
    const seen: string[] = [];
    const out = feedMessage((k) => (seen.push(k), true), "", {});
    expect(out.key).toBe("P1Agents.feed.fallback");
    expect(feedMessage(() => true, "a b/../c", {}).key).toBe("P1Agents.feed.fallback");
    expect(seen).toEqual([]); // a malformed key is never looked up
  });
});

describe("translateFeed", () => {
  it("renders the translated string", () => {
    const t = Object.assign((k: string, a?: Record<string, string>) => `${k}|${a?.kind ?? ""}`, {
      has: (k: string) => k === "P1Agents.feed.intake.classified",
    });
    expect(translateFeed(t, "intake.classified", { kind: "invoice" })).toBe("P1Agents.feed.intake.classified|invoice");
  });

  it("falls back when the translator throws (a key that resolves to a namespace object)", () => {
    const t = Object.assign(
      (k: string) => {
        if (k === "P1Agents.feed.verifier") throw new Error("object");
        return `ok:${k}`;
      },
      { has: () => true },
    );
    expect(translateFeed(t, "verifier", {})).toBe("ok:P1Agents.feed.fallback");
  });
});
