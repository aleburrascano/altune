import { QueryClient, type QueryKey } from '@tanstack/react-query';

type Filters = { queryKey?: QueryKey } | undefined;

export class ObservedQueryClient extends QueryClient {
  readonly cancelled: Filters[] = [];
  readonly invalidated: Filters[] = [];
  readonly written: QueryKey[] = [];
  invalidationHeldUntil: Promise<void> | undefined;
  whenCancelling: (() => void) | undefined;

  constructor() {
    super({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    const write = this.setQueryData.bind(this);
    this.setQueryData = ((key: QueryKey, ...rest: [never, never?]) => {
      this.written.push(key);
      return write(key, ...rest);
    }) as QueryClient['setQueryData'];
  }

  override cancelQueries(...args: Parameters<QueryClient['cancelQueries']>) {
    this.cancelled.push(args[0]);
    this.whenCancelling?.();
    return super.cancelQueries(...args);
  }

  override invalidateQueries(...args: Parameters<QueryClient['invalidateQueries']>) {
    this.invalidated.push(args[0]);
    return this.invalidationHeldUntil ?? super.invalidateQueries(...args);
  }
}
