/** Pure DAG layout for the run drawer: deterministic positions from `depends_on`, no DOM (spec 5.7). */

export type PlanNodeIn = { node_id: string; depends_on: string[] };

export type DagPosition = { id: string; layer: number; x: number; y: number };

export const LAYER_GAP = 220;
export const ROW_GAP = 76;

/**
 * Kahn layering by longest path: a node sits one layer after its deepest dependency. Within a layer, nodes keep
 * input order, so the same plan always gets the same positions. Dependencies on nodes outside the input are
 * ignored. A cycle (a self-reference included) or a duplicate id throws; the server never produces one.
 */
export function layoutDag(nodes: readonly PlanNodeIn[]): DagPosition[] {
  const ids = new Set<string>();
  for (const n of nodes) {
    if (ids.has(n.node_id)) throw new Error(`dag: duplicate node ${n.node_id}`);
    ids.add(n.node_id);
  }
  const deps = new Map<string, string[]>();
  const children = new Map<string, string[]>();
  const waiting = new Map<string, number>();
  for (const n of nodes) {
    const known = [...new Set(n.depends_on)].filter((d) => ids.has(d));
    deps.set(n.node_id, known);
    waiting.set(n.node_id, known.length);
    for (const d of known) children.set(d, [...(children.get(d) ?? []), n.node_id]);
  }

  const layer = new Map<string, number>();
  const queue = nodes.filter((n) => waiting.get(n.node_id) === 0).map((n) => n.node_id);
  for (const id of queue) layer.set(id, 0);
  for (let head = 0; head < queue.length; head++) {
    const id = queue[head];
    for (const child of children.get(id) ?? []) {
      layer.set(child, Math.max(layer.get(child) ?? 0, (layer.get(id) ?? 0) + 1));
      const left = (waiting.get(child) ?? 0) - 1;
      waiting.set(child, left);
      if (left === 0) queue.push(child);
    }
  }
  if (queue.length !== nodes.length) throw new Error("dag: cycle in plan");

  const rowInLayer = new Map<number, number>();
  return nodes.map((n) => {
    const l = layer.get(n.node_id) ?? 0;
    const row = rowInLayer.get(l) ?? 0;
    rowInLayer.set(l, row + 1);
    return { id: n.node_id, layer: l, x: l * LAYER_GAP, y: row * ROW_GAP };
  });
}

export type DagNode = PlanNodeIn & { agent: string; action: string; kind: string; status: string };
type PlanLike = PlanNodeIn & { agent: string; action: string; kind: string };
type StepLike = PlanLike & { status: string; seq: number };

/** The run's plan plus any nodes only its steps know about (dynamic expansion); each carries its latest step status. */
export function dagNodesFromRun(plan: readonly PlanLike[], steps: readonly StepLike[]): DagNode[] {
  const latest = new Map<string, StepLike>();
  for (const s of steps) {
    const prev = latest.get(s.node_id);
    if (!prev || s.seq >= prev.seq) latest.set(s.node_id, s);
  }
  const out: DagNode[] = [];
  const seen = new Set<string>();
  for (const p of plan) {
    if (seen.has(p.node_id)) continue;
    seen.add(p.node_id);
    const s = latest.get(p.node_id);
    out.push({ ...p, depends_on: s?.depends_on ?? p.depends_on, status: s?.status ?? "pending" });
  }
  const extra = [...latest.values()].filter((s) => !seen.has(s.node_id)).sort((a, b) => a.seq - b.seq);
  for (const s of extra) {
    seen.add(s.node_id);
    out.push({ node_id: s.node_id, depends_on: s.depends_on, agent: s.agent, action: s.action, kind: s.kind, status: s.status });
  }
  return out;
}
