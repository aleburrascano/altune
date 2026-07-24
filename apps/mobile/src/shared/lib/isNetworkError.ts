export function isNetworkError(err: unknown): boolean {
  return err instanceof Error && /network|fetch|timeout|connection/i.test(err.message);
}
