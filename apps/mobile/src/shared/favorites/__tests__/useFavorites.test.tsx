import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { useFavorites } from '@shared/favorites/useFavorites';
import type { FavoritesResponse, FavoriteTarget } from '@shared/api-client/favorites';
import { asFavoriteKey } from '@shared/api-client/ids';
import { FavoritesPortProvider } from '@shared/favorites';
import { createMemoryFavoritesPort } from '@shared/favorites/__tests__/memoryFavoritesPort';
import type { FavoritesPort } from '@shared/favorites/ports/favoritesPort';
import { discoveryKeys } from '@shared/lib/query-keys';

let mockedAdd: jest.MockedFunction<FavoritesPort['addFavorite']>;
let mockedList: jest.MockedFunction<FavoritesPort['listFavorites']>;
let mockedRemove: jest.MockedFunction<FavoritesPort['removeFavorite']>;
let port: FavoritesPort;

let alertSpy: jest.SpyInstance;
beforeEach(() => {
  const memory = createMemoryFavoritesPort();
  mockedAdd = jest.fn(memory.addFavorite);
  mockedList = jest.fn(memory.listFavorites);
  mockedRemove = jest.fn(memory.removeFavorite);
  port = { addFavorite: mockedAdd, listFavorites: mockedList, removeFavorite: mockedRemove };
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
});
afterEach(() => {
  alertSpy.mockRestore();
});

const target: FavoriteTarget = {
  kind: 'artist',
  favorite_key: asFavoriteKey('artist-1'),
  title: 'Artist One',
  subtitle: 'Artist',
};

const saved: FavoritesResponse = {
  items: [
    { kind: 'artist', key: asFavoriteKey('artist-1'), title: 'Artist One', subtitle: 'Artist' },
  ],
  total: 1,
};

const addedFavorite = saved.items[0]!;

function newClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function wrapperFor(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <FavoritesPortProvider port={port}>{children}</FavoritesPortProvider>
      </QueryClientProvider>
    );
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function flush(times = 8): Promise<void> {
  for (let i = 0; i < times; i += 1) await Promise.resolve();
}

describe('useFavorites(): optimistic toggle', () => {
  it('prepends the target to the favorites cache mid-flight when it was not saved', async () => {
    const pending = deferred<void>();
    mockedAdd.mockReturnValue(pending.promise.then(() => addedFavorite));
    mockedList.mockResolvedValue({ items: [], total: 0 });
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    expect(result.current.isFavorite(target)).toBe(false);

    act(() => result.current.toggle(target));
    await act(flush);

    expect(queryClient.getQueryData(discoveryKeys.favorites)).toEqual(saved);
    expect(mockedAdd).toHaveBeenCalledWith({
      kind: 'artist',
      title: 'Artist One',
      subtitle: 'Artist',
      image_url: undefined,
    });

    await act(async () => {
      pending.resolve();
      await flush();
    });
  });

  it('removes a saved target mid-flight and calls removeFavorite', async () => {
    const pending = deferred<void>();
    mockedRemove.mockReturnValue(pending.promise);
    mockedList.mockResolvedValue({ items: [], total: 0 });
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, saved);

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    expect(result.current.isFavorite(target)).toBe(true);

    act(() => result.current.toggle(target));
    await act(flush);

    expect(queryClient.getQueryData(discoveryKeys.favorites)).toEqual({ items: [], total: 0 });
    expect(mockedRemove).toHaveBeenCalledTimes(1);
    expect(mockedAdd).not.toHaveBeenCalled();

    await act(async () => {
      pending.resolve();
      await flush();
    });
  });

  it('writes the optimistic entry even against a cold cache (no guard on an empty snapshot)', async () => {
    const pending = deferred<void>();
    mockedAdd.mockReturnValue(pending.promise.then(() => addedFavorite));
    mockedList.mockReturnValue(new Promise(() => {}));
    const queryClient = newClient();

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    act(() => result.current.toggle(target));
    await act(flush);

    expect(queryClient.getQueryData(discoveryKeys.favorites)).toEqual(saved);
    await act(async () => {
      pending.resolve();
      await flush();
    });
  });

  it('on failure rolls back to the snapshot, then refetches the favorites', async () => {
    mockedAdd.mockRejectedValue(new Error('boom'));
    mockedList.mockReturnValue(new Promise(() => {}));
    const queryClient = newClient();
    const snapshot = { items: [], total: 0 };
    queryClient.setQueryData(discoveryKeys.favorites, snapshot);
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    act(() => result.current.toggle(target));
    await act(flush);

    expect(queryClient.getQueryData(discoveryKeys.favorites)).toEqual(snapshot);
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: discoveryKeys.favorites });
  });

  it('alerts when saving a favorite fails, so the rollback is not silent', async () => {
    mockedAdd.mockRejectedValue(new Error('boom'));
    mockedList.mockReturnValue(new Promise(() => {}));
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    act(() => result.current.toggle(target));
    await act(flush);

    expect(alertSpy).toHaveBeenCalledWith(
      'Update failed',
      'Could not update your favorites. Please try again.',
    );
  });

  it('alerts when removing a favorite fails, so the rollback is not silent', async () => {
    mockedRemove.mockRejectedValue(new Error('boom'));
    mockedList.mockReturnValue(new Promise(() => {}));
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, saved);

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    act(() => result.current.toggle(target));
    await act(flush);

    expect(alertSpy).toHaveBeenCalledWith(
      'Update failed',
      'Could not update your favorites. Please try again.',
    );
  });

  it('does not cancel an in-flight favorites fetch before writing optimistically', async () => {
    const pending = deferred<void>();
    mockedAdd.mockReturnValue(pending.promise.then(() => addedFavorite));
    mockedList.mockReturnValue(new Promise(() => {}));
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });
    const cancelSpy = jest.spyOn(queryClient, 'cancelQueries');

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    act(() => result.current.toggle(target));
    await act(flush);

    expect(cancelSpy).not.toHaveBeenCalled();
    await act(async () => {
      pending.resolve();
      await flush();
    });
  });

  it('toggles back on a second tap before re-render instead of duplicating the entry', async () => {
    const pendingAdd = deferred<void>();
    const pendingRemove = deferred<void>();
    mockedAdd.mockReturnValue(pendingAdd.promise.then(() => addedFavorite));
    mockedRemove.mockReturnValue(pendingRemove.promise);
    mockedList.mockReturnValue(new Promise(() => {}));
    const queryClient = newClient();
    queryClient.setQueryData(discoveryKeys.favorites, { items: [], total: 0 });

    const { result } = renderHook(() => useFavorites(), { wrapper: wrapperFor(queryClient) });
    await act(async () => {
      result.current.toggle(target);
      result.current.toggle(target);
      await flush();
    });

    expect(queryClient.getQueryData(discoveryKeys.favorites)).toEqual({ items: [], total: 0 });
    expect(mockedAdd).toHaveBeenCalledTimes(1);
    expect(mockedRemove).toHaveBeenCalledTimes(1);

    await act(async () => {
      pendingAdd.resolve();
      pendingRemove.resolve();
      await flush();
    });
  });
});
