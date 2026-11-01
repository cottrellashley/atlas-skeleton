// Time, injected: every wait and every deadline in this client goes
// through a Clock, so that a test drives time instead of sleeping.

export interface Clock {
  /** Milliseconds since the epoch. */
  now(): number;
  /** Waits ms; rejects with the signal's reason when it aborts first. */
  sleep(ms: number, signal?: AbortSignal): Promise<void>;
  /** Calls fn after ms, unless the returned function is called first. A timer never keeps the process alive. */
  after(ms: number, fn: () => void): () => void;
}

/** The system's clock. */
export const systemClock: Clock = {
  now: () => Date.now(),
  sleep(ms: number, signal?: AbortSignal): Promise<void> {
    return new Promise((resolve, reject) => {
      if (signal?.aborted) { reject(signal.reason); return; }
      const onAbort = () => { clearTimeout(t); reject(signal!.reason); };
      const t = setTimeout(() => { signal?.removeEventListener('abort', onAbort); resolve(); }, Math.max(0, ms));
      signal?.addEventListener('abort', onAbort, { once: true });
    });
  },
  after(ms: number, fn: () => void): () => void {
    const t = setTimeout(fn, Math.max(0, ms));
    t.unref?.();
    return () => clearTimeout(t);
  },
};
