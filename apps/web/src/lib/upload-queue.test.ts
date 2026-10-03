import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { contentTypeOf, runQueue, runUploads, type UploadDeps } from "./upload-queue";
import { uploadReducer, type UploadAction, type UploadItem } from "./uploads";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

describe("runQueue", () => {
  it("never runs more than the concurrency and completes everything", async () => {
    let active = 0;
    let peak = 0;
    const done: number[] = [];
    const p = runQueue([...Array(10).keys()], 4, async (n) => {
      active++;
      peak = Math.max(peak, active);
      await sleep(50);
      active--;
      done.push(n);
    });
    await vi.runAllTimersAsync();
    await p;
    expect(peak).toBe(4);
    expect(done).toHaveLength(10);
  });

  it("holds the bound even when workers resolve instantly", async () => {
    let active = 0;
    let peak = 0;
    await runQueue([...Array(25).keys()], 4, async () => {
      active++;
      peak = Math.max(peak, active);
      await Promise.resolve();
      active--;
    });
    expect(peak).toBeLessThanOrEqual(4);
  });

  it("keeps going when one worker rejects", async () => {
    const done: number[] = [];
    const p = runQueue([1, 2, 3, 4, 5], 2, async (n) => {
      await sleep(10);
      if (n === 2) throw new Error("boom");
      done.push(n);
    });
    await vi.runAllTimersAsync();
    await p;
    expect(done.sort()).toEqual([1, 3, 4, 5]);
  });

  it("handles an empty list and a non-positive concurrency", async () => {
    await runQueue([], 4, async () => {});
    const seen: number[] = [];
    await runQueue([1, 2], 0, async (n) => void seen.push(n));
    expect(seen).toEqual([1, 2]);
  });
});

describe("runUploads", () => {
  const mk = (id: string, name = id): UploadItem => ({
    id,
    file: new File([id], `${name}.pdf`, { type: "application/pdf" }),
    state: "hashing",
    progress: 0,
  });

  function harness(over: Partial<UploadDeps> = {}) {
    vi.useRealTimers();
    let items: UploadItem[] = [mk("a"), mk("b"), mk("c")];
    const log: string[] = [];
    const dispatch = (a: UploadAction) => {
      items = uploadReducer(items, a);
    };
    const deps: UploadDeps = {
      hash: async (f) => `sha-${f.name}`,
      requestUploads: async (_client, files) =>
        files.map((f, i) => ({
          document_id: `doc-${f.sha256}`,
          status: "pending_upload",
          deduplicated: false,
          upload: { url: `https://s3/${i}`, method: "PUT", headers: { "Content-Type": f.content_type }, expires_at: "" },
        })),
      put: async (_u, _f, onProgress) => {
        log.push("put");
        onProgress(0.5);
      },
      complete: async (ids) => {
        log.push(`complete:${ids.length}`);
        return ids.map((id) => ({ document_id: id, status: "uploaded" }));
      },
      ...over,
    };
    return { run: () => runUploads({ items, clientCompanyId: "cc", dispatch, deps }), get: () => items, log };
  }

  it("takes every item through completing to processing", async () => {
    const h = harness();
    await h.run();
    expect(h.get().map((i) => i.state)).toEqual(["processing", "processing", "processing"]);
    expect(h.get().every((i) => i.documentId?.startsWith("doc-"))).toBe(true);
    expect(h.log.filter((l) => l === "put")).toHaveLength(3);
    expect(h.log.filter((l) => l.startsWith("complete"))).toEqual(["complete:3"]);
  });

  it("marks duplicates and never uploads or completes them", async () => {
    const h = harness({
      requestUploads: async (_c, files) =>
        files.map((f) => ({
          document_id: `doc-${f.sha256}`,
          status: "uploaded",
          deduplicated: f.filename !== "a.pdf",
          upload: f.filename === "a.pdf" ? { url: "u", method: "PUT", headers: {}, expires_at: "" } : null,
        })),
    });
    await h.run();
    expect(h.get().map((i) => i.state)).toEqual(["processing", "duplicate", "duplicate"]);
    expect(h.log.filter((l) => l === "put")).toHaveLength(1);
    expect(h.log).toContain("complete:1");
  });

  it("rejects an item the server refused and fails the rest of a failed request", async () => {
    const h = harness({
      requestUploads: async (_c, files) =>
        files.map((f) =>
          f.filename === "b.pdf"
            ? { document_id: null, status: "", deduplicated: false, upload: null, error: "invalid_content_type" }
            : { document_id: `d-${f.filename}`, status: "pending_upload", deduplicated: false,
                upload: { url: "u", method: "PUT", headers: {}, expires_at: "" } }),
    });
    await h.run();
    expect(h.get().map((i) => i.state)).toEqual(["processing", "rejected", "processing"]);
    expect(h.get()[1]!.error).toBe("invalid_content_type");

    const h2 = harness({ requestUploads: async () => { throw new Error("down"); } });
    await h2.run();
    expect(h2.get().map((i) => i.state)).toEqual(["failed", "failed", "failed"]);
  });

  it("fails a file whose PUT fails but still completes the others", async () => {
    let n = 0;
    const h = harness({
      put: async () => {
        if (n++ === 1) throw new Error("put failed");
      },
    });
    await h.run();
    expect(h.get().filter((i) => i.state === "failed")).toHaveLength(1);
    expect(h.get().filter((i) => i.state === "processing")).toHaveLength(2);
    expect(h.log).toContain("complete:2");
  });

  it("maps a rejected completion and a storage error", async () => {
    const h = harness({
      complete: async (ids) => [
        { document_id: ids[0]!, status: "rejected", error: "content_type_mismatch" },
        { document_id: ids[1]!, status: "pending_upload", error: "storage_unavailable" },
        { document_id: ids[2]!, status: "uploaded" },
      ],
    });
    await h.run();
    expect(h.get().map((i) => i.state)).toEqual(["rejected", "failed", "processing"]);
  });

  it("fails an item that cannot be hashed without stopping the others", async () => {
    const h = harness({
      hash: async (f) => {
        if (f.name === "b.pdf") throw new Error("read failed");
        return `sha-${f.name}`;
      },
    });
    await h.run();
    expect(h.get().map((i) => i.state)).toEqual(["processing", "failed", "processing"]);
  });
});

describe("contentTypeOf", () => {
  it("uses the browser type, else the extension", () => {
    expect(contentTypeOf({ name: "a.pdf", type: "application/pdf" })).toBe("application/pdf");
    expect(contentTypeOf({ name: "a.CSV", type: "" })).toBe("text/csv");
    expect(contentTypeOf({ name: "a.xlsx", type: "" })).toContain("spreadsheetml");
    expect(contentTypeOf({ name: "a.bin", type: "" })).toBe("");
  });
});
