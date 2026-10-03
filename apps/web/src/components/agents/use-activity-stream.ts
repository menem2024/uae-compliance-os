"use client";

import { useEffect, useState } from "react";
import { parseSseEvent, type FeedEvent } from "@/lib/agent-feed";

export type Connection = "connecting" | "live" | "offline";

const TYPES = ["run", "step", "proposal", "resync"] as const;
const RETRY_MS = 5000;

/**
 * Opens the BFF event stream and only forwards parsed events to `dispatch`; the reducer owns all feed state.
 * EventSource reconnects by itself and resends Last-Event-ID, so the server's backfill-then-live contract
 * holds across blips. After a `resync` marker the source is replaced, which restarts from a fresh backfill.
 */
export function useActivityStream(dispatch: (event: FeedEvent) => void): Connection {
  const [connection, setConnection] = useState<Connection>("connecting");

  useEffect(() => {
    let source: EventSource | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;

    const connect = () => {
      if (stopped) return;
      const es = new EventSource("/api/agents/activity");
      source = es;
      es.onopen = () => setConnection("live");
      for (const type of TYPES) {
        es.addEventListener(type, (m) => {
          const msg = m as MessageEvent<string>;
          const event = parseSseEvent(type, msg.lastEventId, String(msg.data));
          if (!event) return;
          dispatch(event);
          if (type === "resync") {
            es.close();
            connect();
          }
        });
      }
      es.onerror = () => {
        setConnection("offline");
        // A non-200 (429 too_many_streams, a dropped session) closes the source for good: retry on a timer.
        if (es.readyState === EventSource.CLOSED) {
          es.close();
          timer = setTimeout(connect, RETRY_MS);
        }
      };
    };

    connect();
    return () => {
      stopped = true;
      clearTimeout(timer);
      source?.close();
    };
  }, [dispatch]);

  return connection;
}
