// A clock a test drives (AGENTS.md: inject the clock; never sleep to wait).
// sleep moves time forward at once and fires every timer that falls due,
// so a retry's wait costs nothing and is recorded; advance does the same
// for a test that wants a deadline to pass.

import type { Clock } from '../src/clock.ts';

export class FakeClock implements Clock {
  t: number;
  /** Every wait the client asked for, in order. */
  readonly sleeps: number[] = [];
  private timers: { at: number; fn: () => void }[] = [];

  constructor(start = Date.UTC(2026, 10, 1, 9, 0, 0)) {
    this.t = start;
  }

  now(): number { return this.t; }

  async sleep(ms: number, signal?: AbortSignal): Promise<void> {
    if (signal?.aborted) throw signal.reason;
    this.sleeps.push(ms);
    this.advance(ms);
    if (signal?.aborted) throw signal.reason;
  }

  after(ms: number, fn: () => void): () => void {
    const timer = { at: this.t + Math.max(0, ms), fn };
    this.timers.push(timer);
    return () => { this.timers = this.timers.filter((x) => x !== timer); };
  }

  /** Time passes: every timer due by then fires, in order. */
  advance(ms: number): void {
    this.t += ms;
    const due = this.timers.filter((x) => x.at <= this.t).sort((a, b) => a.at - b.at);
    this.timers = this.timers.filter((x) => x.at > this.t);
    for (const x of due) x.fn();
  }

  /** Timers not yet fired: a call that ended leaves none. */
  pending(): number { return this.timers.length; }
}
