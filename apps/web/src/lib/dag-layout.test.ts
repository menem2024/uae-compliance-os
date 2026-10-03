import { describe, expect, it } from "vitest";
import { dagNodesFromRun, layoutDag, type PlanNodeIn } from "./dag-layout";

const n = (node_id: string, ...depends_on: string[]): PlanNodeIn => ({ node_id, depends_on });
const byId = (out: ReturnType<typeof layoutDag>) => Object.fromEntries(out.map((o) => [o.id, o]));

describe("layoutDag", () => {
  it("returns nothing for no nodes", () => {
    expect(layoutDag([])).toEqual([]);
  });

  it("lays a linear chain out in increasing layers", () => {
    const out = byId(layoutDag([n("a"), n("b", "a"), n("c", "b")]));
    expect([out.a.layer, out.b.layer, out.c.layer]).toEqual([0, 1, 2]);
    expect(out.a.x).toBeLessThan(out.b.x);
    expect(out.b.x).toBeLessThan(out.c.x);
  });

  it("puts the two branches of a diamond in the same layer, on different rows", () => {
    const out = byId(layoutDag([n("fetch"), n("a", "fetch"), n("b", "fetch"), n("join", "a", "b")]));
    expect(out.a.layer).toBe(1);
    expect(out.b.layer).toBe(1);
    expect(out.a.x).toBe(out.b.x);
    expect(out.a.y).not.toBe(out.b.y);
    expect(out.join.layer).toBe(2);
  });

  it("uses the longest path, whatever the input order", () => {
    const out = byId(layoutDag([n("join", "a", "c"), n("c", "b"), n("b", "a"), n("a")]));
    expect(out.join.layer).toBe(3);
  });

  it("is deterministic: same input, same positions, input order within a layer", () => {
    const input = [n("r"), n("z", "r"), n("m", "r")];
    expect(layoutDag(input)).toEqual(layoutDag(input));
    const out = layoutDag(input);
    expect(out.filter((o) => o.layer === 1).map((o) => o.id)).toEqual(["z", "m"]);
  });

  it("ignores dependencies on nodes that are not in the plan", () => {
    expect(byId(layoutDag([n("a", "ghost")])).a.layer).toBe(0);
  });

  it("throws on a self-reference", () => {
    expect(() => layoutDag([n("a", "a")])).toThrow(/cycle/i);
  });

  it("throws on a cycle instead of looping", () => {
    expect(() => layoutDag([n("a", "c"), n("b", "a"), n("c", "b")])).toThrow(/cycle/i);
  });

  it("throws on duplicate node ids", () => {
    expect(() => layoutDag([n("a"), n("a")])).toThrow(/duplicate/i);
  });
});

describe("dagNodesFromRun", () => {
  it("adds nodes that only steps know about (dynamic expansion) and keeps plan order first", () => {
    const nodes = dagNodesFromRun(
      [{ node_id: "a", agent: "intake", action: "classify", kind: "llm", depends_on: [] }],
      [
        { node_id: "a", depends_on: [], status: "succeeded", seq: 1, agent: "intake", action: "classify", kind: "llm" },
        { node_id: "a.critic", depends_on: ["a"], status: "started", seq: 2, agent: "verifier", action: "critic", kind: "llm" },
        { node_id: "a.critic", depends_on: ["a"], status: "succeeded", seq: 3, agent: "verifier", action: "critic", kind: "llm" },
      ],
    );
    expect(nodes.map((x) => x.node_id)).toEqual(["a", "a.critic"]);
    expect(nodes[0]).toMatchObject({ status: "succeeded", agent: "intake" });
    expect(nodes[1]).toMatchObject({ status: "succeeded", depends_on: ["a"] }); // latest seq wins
  });

  it("marks plan nodes with no step yet as pending", () => {
    const nodes = dagNodesFromRun([{ node_id: "a", agent: "x", action: "y", kind: "tool", depends_on: [] }], []);
    expect(nodes[0].status).toBe("pending");
  });
});
