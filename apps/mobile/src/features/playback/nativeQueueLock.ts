let chain: Promise<unknown> = Promise.resolve();

export function withNativeQueue<T>(op: () => Promise<T>): Promise<T> {
  const run = chain.then(op, op);
  chain = run.catch(() => undefined);
  return run;
}
