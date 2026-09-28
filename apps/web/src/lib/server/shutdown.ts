import { trace } from "@opentelemetry/api";

type Flushable = { forceFlush?: () => Promise<void>; shutdown?: () => Promise<void> };

let installed = false;

/**
 * Graceful SIGTERM/SIGINT for the container (plan rule: `docker stop` exits 0 in < 10s).
 * Next's built-in handler exits 143, so the image sets NEXT_MANUAL_SIG_HANDLE=true and this
 * handler takes over: flush + shut down the OTel tracer provider (bounded), then exit 0.
 * Without NEXT_MANUAL_SIG_HANDLE (e.g. `next dev`) Next keeps handling signals itself.
 */
export function installShutdownHooks(): void {
  if (installed || !process.env.NEXT_MANUAL_SIG_HANDLE) return;
  installed = true;

  let stopping = false;
  const stop = async (signal: NodeJS.Signals) => {
    if (stopping) return;
    stopping = true;
    console.log(`${signal} received: flushing telemetry and exiting`);
    const provider = trace.getTracerProvider() as Flushable & { getDelegate?: () => Flushable };
    const delegate = provider.getDelegate?.() ?? provider;
    const timeout = new Promise<void>((resolve) => setTimeout(resolve, 3000).unref());
    try {
      await Promise.race([
        (async () => {
          await delegate.forceFlush?.();
          await delegate.shutdown?.();
        })(),
        timeout,
      ]);
    } catch (err) {
      console.warn("telemetry flush failed", err instanceof Error ? err.message : err);
    }
    process.exit(0);
  };

  process.once("SIGTERM", () => void stop("SIGTERM"));
  process.once("SIGINT", () => void stop("SIGINT"));
}
