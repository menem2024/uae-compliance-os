import { describe, expect, it } from "vitest";
import {
  agentFeedReducer, initialFeedState, MAX_LINES, parseSseEvent, type FeedEvent, type FeedState,
} from "./agent-feed";

const step = (over: Record<string, unknown> = {}) => ({
  id: "s1", run_id: "r1", node_id: "n1", agent: "intake", action: "classify", kind: "llm", status: "succeeded",
  message_key: "intake.classified", message_args: { kind: "invoice" }, seq: 1, at: "2026-01-01T00:00:00Z", ...over,
});
const run = (over: Record<string, unknown> = {}) => ({
  id: "r1", workflow: "document_ingestion@1", subject_type: "document", subject_id: "d1", status: "running",
  plan: [{ node_id: "n1", agent: "intake", action: "classify", kind: "llm", depends_on: [] }],
  totals: { steps: 0 }, started_at: "2026-01-01T00:00:00Z", ...over,
});
const ev = (type: FeedEvent["type"], id: string, data: unknown): FeedEvent => ({ type, id, data });
const apply = (events: FeedEvent[], from: FeedState = initialFeedState) => events.reduce(agentFeedReducer, from);

describe("agentFeedReducer", () => {
  it("appends a step as a feed line carrying its message key and args", () => {
    const s = apply([ev("step", "1-a", step())]);
    expect(s.lines).toHaveLength(1);
    expect(s.lines[0]).toMatchObject({ id: "1-a", runId: "r1", agent: "intake", key: "intake.classified", args: { kind: "invoice" } });
  });

  it("applies two events with the same id once", () => {
    const e = ev("step", "1-a", step());
    const s = apply([e, e]);
    expect(s.lines).toHaveLength(1);
    const r = ev("run", "1-r", run());
    expect(apply([r, r]).runs).toHaveLength(1);
  });

  it("a step for an unknown run_id still appends to lines but not to runs", () => {
    const s = apply([ev("step", "1-a", step({ run_id: "ghost" }))]);
    expect(s.lines).toHaveLength(1);
    expect(s.runs).toHaveLength(0);
  });

  it("gives step lines without a message_key the generic `step` template", () => {
    const s = apply([ev("step", "1-a", step({ message_key: "", message_args: undefined, status: "started" }))]);
    expect(s.lines[0].key).toBe("step");
    expect(s.lines[0].args).toMatchObject({ agent: "intake", action: "classify", status: "started" });
  });

  it("upserts a run by its id and keeps the plan when a partial finished view arrives", () => {
    const s = apply([
      ev("run", "1-r", run()),
      ev("run", "2-r", { id: "r1", workflow: "", status: "succeeded", plan: [], totals: { steps: 5 },
        started_at: "0001-01-01T00:00:00Z", finished_at: "2026-01-01T00:00:09Z" }),
    ]);
    expect(s.runs).toHaveLength(1);
    expect(s.runs[0]).toMatchObject({ status: "succeeded", workflow: "document_ingestion@1", started_at: "2026-01-01T00:00:00Z" });
    expect(s.runs[0].plan).toHaveLength(1);
    expect(s.runs[0].totals.steps).toBe(5);
    expect(s.runs[0].finished_at).toBe("2026-01-01T00:00:09Z");
  });

  it("upserts proposals by id", () => {
    const p = { id: "p1", agent: "extraction", kind: "fix", state: "proposed", confidence: 0.9 };
    const s = apply([ev("proposal", "1-p", p), ev("proposal", "2-p", { ...p, state: "accepted" })]);
    expect(s.proposals).toHaveLength(1);
    expect(s.proposals[0].state).toBe("accepted");
  });

  it("ignores malformed payloads", () => {
    const s = apply([ev("step", "1-a", null), ev("run", "1-b", { nope: 1 }), ev("proposal", "1-c", 7)]);
    expect(s).toEqual(initialFeedState);
  });

  it("resync clears lines, runs and proposals and flags needsBackfill", () => {
    const filled = apply([ev("step", "1-a", step()), ev("run", "1-r", run()), ev("proposal", "1-p", { id: "p1", state: "proposed" })]);
    const s = agentFeedReducer(filled, ev("resync", "resync-1", {}));
    expect(s.lines).toEqual([]);
    expect(s.runs).toEqual([]);
    expect(s.proposals).toEqual([]);
    expect(s.needsBackfill).toBe(true);
  });

  it("a subsequent backfill batch clears the flag and may re-apply previously seen ids", () => {
    const e = ev("step", "1-a", step());
    const resynced = agentFeedReducer(apply([e]), ev("resync", "resync-1", {}));
    const s = apply([ev("run", "2-r", run()), e], resynced);
    expect(s.needsBackfill).toBe(false);
    expect(s.lines).toHaveLength(1); // the seen-set was reset with the buffers
  });

  it("keeps a bounded buffer: the oldest lines drop first", () => {
    const events = Array.from({ length: MAX_LINES + 25 }, (_, i) => ev("step", `${i}-x`, step({ id: `s${i}`, seq: i })));
    const s = apply(events);
    expect(s.lines).toHaveLength(MAX_LINES);
    expect(s.lines[0].id).toBe("25-x");
    expect(s.lines.at(-1)?.id).toBe(`${MAX_LINES + 24}-x`);
    // an id that already fell out of the seen-set window is not an issue; a recent one still dedupes
    expect(apply([ev("step", `${MAX_LINES + 24}-x`, step())], s).lines).toHaveLength(MAX_LINES);
  });
});

describe("parseSseEvent", () => {
  it("parses a typed event with its id", () => {
    expect(parseSseEvent("step", "1-a", '{"id":"s1"}')).toEqual({ type: "step", id: "1-a", data: { id: "s1" } });
  });
  it("synthesises an id for a resync marker (the server sends none)", () => {
    const e = parseSseEvent("resync", "", "{}");
    expect(e?.type).toBe("resync");
    expect(e?.id).toMatch(/^resync-/);
  });
  it("drops unknown types and invalid JSON", () => {
    expect(parseSseEvent("ping", "1", "{}")).toBeNull();
    expect(parseSseEvent("step", "1-a", "{oops")).toBeNull();
  });
  it("drops non-resync events with no id", () => {
    expect(parseSseEvent("step", "", "{}")).toBeNull();
  });
});
