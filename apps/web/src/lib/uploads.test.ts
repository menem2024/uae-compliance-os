import { describe, expect, it } from "vitest";
import {
  isTerminal, uploadReducer, type DocumentRow, type UploadAction, type UploadItem, documentsNeedPolling, formatBytes, settleFromRows,
} from "./uploads";

const file = new File(["x"], "inv.pdf", { type: "application/pdf" });
const fresh = (id = "a"): UploadItem => ({ id, file, state: "hashing", progress: 0 });
const run = (items: UploadItem[], ...actions: UploadAction[]) => actions.reduce(uploadReducer, items);
const only = (items: UploadItem[]) => items[0]!;

describe("uploadReducer", () => {
  it("drives one item through the happy path to extracted", () => {
    const states: string[] = [];
    let items = [fresh()];
    const steps: UploadAction[] = [
      { type: "Hashed", id: "a" },
      { type: "Requested", id: "a", documentId: "d1" },
      { type: "UploadProgress", id: "a", progress: 0.5 },
      { type: "Uploaded", id: "a" },
      { type: "Completed", id: "a", status: "uploaded" },
      { type: "StatusChanged", id: "a", status: "extracted" },
    ];
    for (const s of steps) {
      items = uploadReducer(items, s);
      states.push(only(items).state);
    }
    expect(states).toEqual(["requesting", "uploading", "uploading", "completing", "processing", "extracted"]);
    expect(only(items)).toMatchObject({ documentId: "d1", progress: 1 });
    expect(isTerminal(only(items).state)).toBe(true);
  });

  it("short-circuits a duplicate without ever uploading", () => {
    const seen: string[] = [];
    let items = [fresh()];
    for (const a of [
      { type: "Hashed", id: "a" },
      { type: "Duplicate", id: "a", documentId: "d9" },
    ] as UploadAction[]) {
      items = uploadReducer(items, a);
      seen.push(only(items).state);
    }
    expect(seen).toEqual(["requesting", "duplicate"]);
    expect(only(items).documentId).toBe("d9");
  });

  it("moves to failed or rejected on Errored from any non-terminal state, then ignores everything", () => {
    for (const from of ["hashing", "requesting", "uploading", "completing", "processing"] as const) {
      const items = [{ ...fresh(), state: from }];
      const failed = run(items, { type: "Errored", id: "a", error: "boom", terminal: "failed" });
      expect(only(failed)).toMatchObject({ state: "failed", error: "boom" });
      const rejected = run(items, { type: "Errored", id: "a", error: "invalid_size", terminal: "rejected" });
      expect(only(rejected).state).toBe("rejected");
      const after = run(failed, { type: "Hashed", id: "a" }, { type: "StatusChanged", id: "a", status: "extracted" });
      expect(after).toEqual(failed);
    }
  });

  it("never reaches a terminal state without completing unless duplicate or errored", () => {
    // Each out-of-order action is a no-op, so a terminal status cannot be reached early.
    const early = run([fresh()], { type: "StatusChanged", id: "a", status: "extracted" });
    expect(only(early).state).toBe("hashing");
    const requesting = run([fresh()], { type: "Hashed", id: "a" }, { type: "StatusChanged", id: "a", status: "extracted" });
    expect(only(requesting).state).toBe("requesting");
    const uploading = run(
      [fresh()],
      { type: "Hashed", id: "a" },
      { type: "Requested", id: "a", documentId: "d" },
      { type: "Completed", id: "a", status: "uploaded" },
      { type: "StatusChanged", id: "a", status: "needs_review" },
    );
    expect(only(uploading).state).toBe("uploading");
  });

  it("maps a rejected completion to rejected and keeps the reason", () => {
    const items = run(
      [{ ...fresh(), state: "completing", documentId: "d" }],
      { type: "Completed", id: "a", status: "rejected", error: "content_type_mismatch" },
    );
    expect(only(items)).toMatchObject({ state: "rejected", error: "content_type_mismatch" });
  });

  it("ignores in-flight statuses and unknown ids", () => {
    const items = [{ ...fresh(), state: "processing" as const }];
    expect(run(items, { type: "StatusChanged", id: "a", status: "processing" })).toEqual(items);
    expect(run(items, { type: "StatusChanged", id: "a", status: "uploaded" })).toEqual(items);
    expect(run(items, { type: "StatusChanged", id: "zzz", status: "extracted" })).toEqual(items);
  });

  it("only touches the item an action addresses", () => {
    const items = [fresh("a"), fresh("b")];
    const out = run(items, { type: "Hashed", id: "b" });
    expect(out.map((i) => i.state)).toEqual(["hashing", "requesting"]);
  });
});

describe("polling helpers", () => {
  const now = Date.parse("2026-10-02T12:00:00Z");
  const ago = (min: number) => new Date(now - min * 60_000).toISOString();
  const row = (id: string, status: string, updated = ago(1)): DocumentRow =>
    ({ id, status, updated_at: updated }) as DocumentRow;
  it("polls while any row is non-terminal and stops once all are terminal", () => {
    expect(documentsNeedPolling([row("1", "extracted"), row("2", "processing")], [], now)).toBe(true);
    expect(documentsNeedPolling([row("1", "uploaded")], [], now)).toBe(true);
    expect(documentsNeedPolling([row("1", "pending_upload")], [], now)).toBe(true);
    expect(documentsNeedPolling([row("1", "extracted"), row("2", "failed"), row("3", "needs_review")], [], now)).toBe(false);
    expect(documentsNeedPolling([], [], now)).toBe(false);
  });
  it("gives up on rows that have been stuck for longer than the window", () => {
    expect(documentsNeedPolling([row("1", "processing", ago(31))], [], now)).toBe(false);
    expect(documentsNeedPolling([row("1", "pending_upload", ago(29))], [], now)).toBe(true);
  });
  it("also polls while an upload item is waiting on the pipeline", () => {
    expect(documentsNeedPolling([], [{ ...fresh(), state: "processing" }], now)).toBe(true);
    expect(documentsNeedPolling([], [{ ...fresh(), state: "uploading" }], now)).toBe(false);
    expect(documentsNeedPolling([], [{ ...fresh(), state: "extracted" }], now)).toBe(false);
  });
});

describe("formatBytes", () => {
  it("uses decimal units", () => {
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1500)).toBe("1.5 KB");
    expect(formatBytes(20_971_520)).toBe("21 MB");
  });
});

describe("settleFromRows", () => {
  it("settles processing items from their Document status and leaves the rest", () => {
    const items: UploadItem[] = [
      { ...fresh("a"), state: "processing", documentId: "d1" },
      { ...fresh("b"), state: "processing", documentId: "d2" },
      { ...fresh("c"), state: "uploading", documentId: "d3" },
    ];
    const out = settleFromRows(items, [
      { id: "d1", status: "needs_review" },
      { id: "d2", status: "processing" },
      { id: "d3", status: "extracted" },
    ]);
    expect(out.map((i) => i.state)).toEqual(["needs_review", "processing", "uploading"]);
    expect(settleFromRows(items, [])).toBe(items);
  });
});
