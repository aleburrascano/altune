import { describe, it, expect } from "vitest";
import { initialConnection, nextConnection, type Connection } from "./useConnection";

const liveAt = (at: number): Connection => ({ conn: "live", lastFrameAt: at });

describe("nextConnection", () => {
  it("goes live on any frame, whatever the connection was before", () => {
    const before: Connection[] = [initialConnection, { conn: "error", lastFrameAt: 1 }, { conn: "stalled", lastFrameAt: 1 }];

    const after = before.map((current) => nextConnection(current, { kind: "frame", at: 50 }));

    expect(after).toEqual(before.map(() => liveAt(50)));
  });

  it("goes to error on a failure and keeps the last frame time", () => {
    expect(nextConnection(liveAt(40), { kind: "failed" })).toEqual({ conn: "error", lastFrameAt: 40 });
  });

  it("stalls when the silence started at the latest frame", () => {
    expect(nextConnection(liveAt(40), { kind: "silent", since: 40 })).toEqual({ conn: "stalled", lastFrameAt: 40 });
  });

  it("ignores a silence armed for an older frame that a newer frame already replaced", () => {
    expect(nextConnection(liveAt(41), { kind: "silent", since: 40 })).toEqual(liveAt(41));
  });

  it("never turns an error or a pending connection into stalled", () => {
    const errored: Connection = { conn: "error", lastFrameAt: 40 };

    expect(nextConnection(errored, { kind: "silent", since: 40 })).toBe(errored);
    expect(nextConnection(initialConnection, { kind: "silent", since: 0 })).toBe(initialConnection);
  });
});
