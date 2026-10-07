"use client";

import { useReducer, useState } from "react";
import { agentFeedReducer, initialFeedState } from "@/lib/agent-feed";
import { ActivityFeed } from "./activity-feed";
import { OrchestraHeader } from "./orchestra-header";
import { ProposalsList } from "./proposals-list";
import { RosterCards } from "./roster-cards";
import { RunDrawer } from "./run-drawer";
import { RunList } from "./run-list";
import { useActivityStream } from "./use-activity-stream";

/** /agents: all feed state lives in the reducer; the stream hook only dispatches into it. */
export function AgentsView() {
  const [feed, dispatch] = useReducer(agentFeedReducer, initialFeedState);
  const connection = useActivityStream(dispatch);
  const [openRun, setOpenRun] = useState<string | null>(null);

  return (
    <div className="flex flex-col gap-8">
      <OrchestraHeader connection={connection} catchingUp={feed.needsBackfill} />
      <RosterCards />
      <ActivityFeed lines={feed.lines} />
      <RunList rows={feed.runs} selectedId={openRun} onOpen={setOpenRun} />
      <ProposalsList rows={feed.proposals} />
      <RunDrawer runId={openRun} onClose={() => setOpenRun(null)} />
    </div>
  );
}
