/** Pure reducer behind the live /agents feed: parsed SSE events in, bounded view state out (spec 5.7, Task 19). */

export type PlanNode = { node_id: string; agent: string; action: string; kind: string; depends_on: string[] };
export type RunTotals = {
  steps: number; llm_calls: number; response_cache_hits: number; input_tokens: number; output_tokens: number;
  cost_micro_usd: number;
};
export type RunBudget = { max_steps: number; max_llm_calls: number; max_cost_micro_usd: number; deadline_seconds: number };
export type RunRow = {
  id: string;
  workflow: string;
  subject_type: string;
  subject_id: string;
  client_company_id?: string;
  status: string;
  error_code?: string;
  plan: PlanNode[];
  budget?: RunBudget;
  totals: RunTotals;
  started_at: string;
  finished_at?: string | null;
};
export type StepRow = {
  id: string; run_id: string; node_id: string; agent: string; action: string; kind: string; status: string;
  message_key?: string; message_args?: Record<string, string>; error_code?: string; depends_on: string[];
  seq: number; attempt?: number; at: string; duration_ms?: number;
  usage?: { model?: string; llm_calls: number; cost_micro_usd: number; response_cache_hit?: boolean };
};
export type ProposalRow = {
  id: string; run_id?: string | null; client_company_id?: string | null; agent: string; kind: string;
  target_type?: string; summary_key?: string; summary_args?: Record<string, string>; rationale?: string;
  confidence: number; state: string; created_at?: string;
};

export type FeedLine = {
  /** The SSE event id (`<unix_ms>-<uuid>`). */
  id: string;
  runId: string;
  nodeId: string;
  agent: string;
  action: string;
  status: string;
  /** `message_key`, or `step` (the generic template) when the step carried none. */
  key: string;
  args: Record<string, string>;
  errorCode?: string;
  at: string;
};

export type FeedEvent = { type: "run" | "step" | "proposal" | "resync"; id: string; data: unknown };

export type FeedState = {
  lines: FeedLine[];
  runs: RunRow[];
  proposals: ProposalRow[];
  /** Set by `resync`, cleared by the next event: the buffers were dropped and a fresh backfill is on its way. */
  needsBackfill: boolean;
  /** Event ids already applied, bounded. Reset with the buffers so a post-resync backfill re-applies. */
  seen: ReadonlySet<string>;
};

// Client-side bounds, the same order of magnitude as the server's per-connection buffer, so the virtualizer
// and the run table never see an unbounded list however long the page stays open.
export const MAX_LINES = 200;
export const MAX_RUNS = 100;
export const MAX_PROPOSALS = 100;
const MAX_SEEN = MAX_LINES * 2;

export const initialFeedState: FeedState = {
  lines: [], runs: [], proposals: [], needsBackfill: false, seen: new Set(),
};

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const str = (v: unknown, d = ""): string => (typeof v === "string" ? v : d);
const num = (v: unknown, d = 0): number => (typeof v === "number" && Number.isFinite(v) ? v : d);
const strMap = (v: unknown): Record<string, string> =>
  isObj(v) ? Object.fromEntries(Object.entries(v).filter((e): e is [string, string] => typeof e[1] === "string")) : {};
/** Go's zero time.Time marshals as 0001-01-01; a partial live view carries it for fields it does not know. */
const realTime = (v: unknown): v is string => typeof v === "string" && v !== "" && !v.startsWith("0001-");

function toPlan(v: unknown): PlanNode[] {
  if (!Array.isArray(v)) return [];
  return v.filter(isObj).map((n) => ({
    node_id: str(n.node_id), agent: str(n.agent), action: str(n.action), kind: str(n.kind),
    depends_on: Array.isArray(n.depends_on) ? n.depends_on.filter((d): d is string => typeof d === "string") : [],
  }));
}

function toTotals(v: unknown): RunTotals {
  const o = isObj(v) ? v : {};
  return {
    steps: num(o.steps), llm_calls: num(o.llm_calls), response_cache_hits: num(o.response_cache_hits),
    input_tokens: num(o.input_tokens), output_tokens: num(o.output_tokens), cost_micro_usd: num(o.cost_micro_usd),
  };
}

function toRun(v: unknown): RunRow | null {
  if (!isObj(v) || typeof v.id !== "string" || v.id === "") return null;
  const b = isObj(v.budget) ? v.budget : null;
  return {
    id: v.id, workflow: str(v.workflow), subject_type: str(v.subject_type), subject_id: str(v.subject_id),
    client_company_id: str(v.client_company_id) || undefined, status: str(v.status, "running"),
    error_code: str(v.error_code) || undefined, plan: toPlan(v.plan),
    budget: b ? {
      max_steps: num(b.max_steps), max_llm_calls: num(b.max_llm_calls),
      max_cost_micro_usd: num(b.max_cost_micro_usd), deadline_seconds: num(b.deadline_seconds),
    } : undefined,
    totals: toTotals(v.totals), started_at: str(v.started_at),
    finished_at: realTime(v.finished_at) ? v.finished_at : null,
  };
}

/** A run update replaces status/totals/error but never blanks what only the first view carried (plan, workflow). */
function mergeRun(prev: RunRow | undefined, next: RunRow): RunRow {
  if (!prev) return next;
  return {
    ...prev,
    workflow: next.workflow || prev.workflow,
    subject_type: next.subject_type || prev.subject_type,
    subject_id: next.subject_id || prev.subject_id,
    client_company_id: next.client_company_id ?? prev.client_company_id,
    status: next.status,
    error_code: next.error_code,
    plan: next.plan.length > 0 ? next.plan : prev.plan,
    budget: next.budget && next.budget.max_steps > 0 ? next.budget : prev.budget,
    totals: next.totals.steps > 0 || next.totals.llm_calls > 0 ? next.totals : prev.totals,
    started_at: realTime(next.started_at) ? next.started_at : prev.started_at,
    finished_at: next.finished_at ?? prev.finished_at,
  };
}

function toLine(eventId: string, v: unknown): FeedLine | null {
  if (!isObj(v) || typeof v.id !== "string" || typeof v.run_id !== "string") return null;
  const agent = str(v.agent);
  const action = str(v.action);
  const status = str(v.status);
  return {
    id: eventId, runId: v.run_id, nodeId: str(v.node_id), agent, action, status,
    key: str(v.message_key) || "step",
    args: { agent, action, status, ...strMap(v.message_args) },
    errorCode: str(v.error_code) || undefined,
    at: str(v.at),
  };
}

function toProposal(v: unknown): ProposalRow | null {
  if (!isObj(v) || typeof v.id !== "string" || v.id === "") return null;
  return {
    id: v.id, run_id: str(v.run_id) || null, client_company_id: str(v.client_company_id) || null,
    agent: str(v.agent), kind: str(v.kind), target_type: str(v.target_type) || undefined,
    summary_key: str(v.summary_key) || undefined, summary_args: strMap(v.summary_args),
    rationale: str(v.rationale) || undefined, confidence: num(v.confidence), state: str(v.state, "proposed"),
    created_at: str(v.created_at) || undefined,
  };
}

function upsert<T extends { id: string }>(list: readonly T[], item: T, merge: (prev: T | undefined, next: T) => T, cap: number): T[] {
  const i = list.findIndex((x) => x.id === item.id);
  const out = i < 0 ? [...list, item] : list.map((x, j) => (j === i ? merge(x, item) : x));
  return out.length > cap ? out.slice(out.length - cap) : out;
}

function remember(seen: ReadonlySet<string>, id: string): ReadonlySet<string> {
  const next = new Set(seen);
  next.add(id);
  for (const old of next) {
    if (next.size <= MAX_SEEN) break;
    next.delete(old);
  }
  return next;
}

/**
 * Backfill, then live; the client dedupes by event id (the server contract). `resync` means the server
 * dropped events for a slow reader: clear everything and wait for a fresh backfill. The next event of any
 * other type clears `needsBackfill`.
 */
export function agentFeedReducer(state: FeedState, event: FeedEvent): FeedState {
  if (event.type === "resync") {
    return { ...initialFeedState, needsBackfill: true };
  }
  if (state.seen.has(event.id)) return state;

  let next: FeedState | null = null;
  if (event.type === "step") {
    const line = toLine(event.id, event.data);
    if (line) {
      const lines = [...state.lines, line];
      next = { ...state, lines: lines.length > MAX_LINES ? lines.slice(lines.length - MAX_LINES) : lines };
    }
  } else if (event.type === "run") {
    const run = toRun(event.data);
    if (run) {
      next = { ...state, runs: upsert(state.runs, run, mergeRun, MAX_RUNS) };
    }
  } else if (event.type === "proposal") {
    const p = toProposal(event.data);
    if (p) {
      next = { ...state, proposals: upsert(state.proposals, p, (_prev, n) => n, MAX_PROPOSALS) };
    }
  }
  if (!next) return state;
  return { ...next, needsBackfill: false, seen: remember(state.seen, event.id) };
}

const TYPES = new Set<FeedEvent["type"]>(["run", "step", "proposal", "resync"]);

/** An EventSource message to a FeedEvent; null for unknown types, bad JSON, or a non-resync event with no id. */
export function parseSseEvent(type: string, lastEventId: string, raw: string): FeedEvent | null {
  if (!TYPES.has(type as FeedEvent["type"])) return null;
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return null;
  }
  if (type === "resync") return { type: "resync", id: lastEventId || `resync-${Date.now()}`, data };
  if (!lastEventId) return null;
  return { type: type as FeedEvent["type"], id: lastEventId, data };
}
