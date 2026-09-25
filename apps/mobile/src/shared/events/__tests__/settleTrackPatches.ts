/**
 * Waits for the batched pass that applies scheduled track-cache patches (#1796). Two turns
 * of the microtask queue is after the flush whichever turn its scheduling promise ran on.
 */
export async function settleTrackPatches(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}
