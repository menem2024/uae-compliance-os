"use client";

import { useTranslations } from "next-intl";
import { useMemo } from "react";
import { dagNodesFromRun, layoutDag, ROW_GAP } from "@/lib/dag-layout";
import type { PlanNode, StepRow } from "@/lib/agent-feed";
import { cn } from "@/lib/utils";

const NODE_W = 180;
const NODE_H = 52;

const TONE: Record<string, string> = {
  started: "border-info bg-info-soft",
  retrying: "border-warn bg-warn-soft",
  succeeded: "border-ok bg-ok-soft",
  failed: "border-bad bg-bad-soft",
  skipped: "border-border bg-muted",
  pending: "border-border bg-panel",
};

/** The run's plan as a left-to-right graph; positions come from the pure `layoutDag`, this only draws them. */
export function DagView({ plan, steps }: { plan: readonly PlanNode[]; steps: readonly StepRow[] }) {
  const t = useTranslations("P1Agents");
  const graph = useMemo(() => {
    try {
      const nodes = dagNodesFromRun(plan, steps);
      const pos = new Map(layoutDag(nodes).map((p) => [p.id, p]));
      return { nodes, pos };
    } catch {
      return null; // a cyclic or duplicated plan: never expected from the server, never crash the drawer
    }
  }, [plan, steps]);

  if (!graph) return <p className="text-sm text-bad">{t("drawer.dagError")}</p>;
  const { nodes, pos } = graph;
  if (nodes.length === 0) return null;

  const width = Math.max(...[...pos.values()].map((p) => p.x)) + NODE_W;
  const height = Math.max(...[...pos.values()].map((p) => p.y)) + Math.max(NODE_H, ROW_GAP - 24);

  return (
    <div className="overflow-x-auto rounded-xl border bg-panel p-3" dir="ltr">
      <div className="relative" style={{ width, height }}>
        <svg className="pointer-events-none absolute inset-0 text-muted-foreground" width={width} height={height} aria-hidden>
          {nodes.flatMap((n) =>
            n.depends_on.flatMap((d) => {
              const from = pos.get(d);
              const to = pos.get(n.node_id);
              if (!from || !to) return [];
              const x1 = from.x + NODE_W;
              const y1 = from.y + NODE_H / 2;
              const x2 = to.x;
              const y2 = to.y + NODE_H / 2;
              const mid = (x1 + x2) / 2;
              return [
                <path
                  key={`${d}>${n.node_id}`}
                  d={`M${x1},${y1} C${mid},${y1} ${mid},${y2} ${x2},${y2}`}
                  fill="none"
                  stroke="currentColor"
                  strokeOpacity={0.5}
                  strokeWidth={1.5}
                />,
              ];
            }),
          )}
        </svg>
        {nodes.map((n) => {
          const p = pos.get(n.node_id);
          if (!p) return null;
          return (
            <div
              key={n.node_id}
              data-testid="dag-node"
              data-node-id={n.node_id}
              data-status={n.status}
              className={cn("absolute flex flex-col justify-center rounded-lg border px-3 text-xs", TONE[n.status] ?? TONE.pending)}
              style={{ left: p.x, top: p.y, width: NODE_W, height: NODE_H }}
              title={n.node_id}
            >
              <span className="truncate font-semibold">{n.agent || n.node_id}</span>
              <span className="truncate text-muted-foreground">{n.action}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
