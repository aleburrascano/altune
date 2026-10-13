import { createElement, type ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { asPlaylistId } from '@shared/apiClient/ids';
import type { PlaylistResponse } from '@shared/apiClient/playlistResponse';
import { playlistKeys } from '@shared/query/queryKeys';
import { signInTestUser } from '@testing/signInTestUser';

import { usePlaylistRename } from '@features/library/hooks/usePlaylistRename';

const { __http } = require('@testing/doubles/fetch.js');

const PLAYLIST_ID = asPlaylistId('pl1');
const CURRENT_NAME = 'Old';
const RENAME_ROUTE = 'PATCH /v1/playlists/pl1';
const TELEMETRY_ROUTE = 'POST /v1/discovery/events';
const HTTP_OK = 200;
const HTTP_ACCEPTED = 202;

interface RecordedRequest {
  method: string;
  body: string;
}

interface WrapperProps {
  children: ReactNode;
}

interface CachedDetail {
  name: string;
  tracks: [];
}

function renamed(name: string): PlaylistResponse {
  return {
    id: PLAYLIST_ID,
    name,
    track_count: 0,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  };
}

let queryClient: QueryClient;

function wrapper({ children }: WrapperProps) {
  return createElement(QueryClientProvider, { client: queryClient }, children);
}

function renderRename(currentName: string | undefined) {
  return renderHook(() => usePlaylistRename(PLAYLIST_ID, currentName), { wrapper }).result;
}

function editingWith(nextName: string) {
  const result = renderRename(CURRENT_NAME);
  act(() => result.current.startEditing());
  act(() => result.current.setEditName(nextName));
  return result;
}

function sentNames(): string[] {
  const requests: RecordedRequest[] = __http.requests;
  return requests
    .filter((request) => request.method === 'PATCH')
    .map((request) => JSON.parse(request.body).name);
}

function cachedName(): string | undefined {
  return queryClient.getQueryData<CachedDetail>(playlistKeys.detail(PLAYLIST_ID))?.name;
}

beforeEach(async () => {
  await signInTestUser();
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  queryClient.setQueryData<CachedDetail>(playlistKeys.detail(PLAYLIST_ID), {
    name: CURRENT_NAME,
    tracks: [],
  });
  __http.reply(RENAME_ROUTE, { status: HTTP_OK, json: renamed('New') });
  __http.reply(TELEMETRY_ROUTE, { status: HTTP_ACCEPTED });
});

afterEach(() => queryClient.clear());

describe('usePlaylistRename', () => {
  it('does nothing when the playlist is not loaded', () => {
    const result = renderRename(undefined);
    act(() => result.current.startEditing());
    expect(result.current.isEditing).toBe(false);
  });

  it('seeds the edit name and renames with the trimmed value, closing on settle', async () => {
    const result = renderRename(CURRENT_NAME);
    act(() => result.current.startEditing());
    expect(result.current).toMatchObject({ isEditing: true, editName: 'Old' });

    act(() => result.current.setEditName('  New  '));
    act(() => result.current.confirmRename());
    expect(result.current.isEditing).toBe(true);

    await waitFor(() => expect(result.current.isEditing).toBe(false));
    expect(sentNames()).toEqual(['New']);
    expect(cachedName()).toBe('New');
  });

  it.each([['   '], ['Old'], [' Old ']])('closes without renaming for %j', (name) => {
    const result = renderRename(CURRENT_NAME);
    act(() => result.current.startEditing());
    act(() => result.current.setEditName(name));
    act(() => result.current.confirmRename());
    expect(__http.countFor(RENAME_ROUTE)).toBe(0);
    expect(cachedName()).toBe(CURRENT_NAME);
    expect(result.current.isEditing).toBe(false);
  });
});

describe('usePlaylistRename — one rename per gesture', () => {
  it('sends a single rename when Return and the blur it triggers confirm in the same tick', async () => {
    const result = editingWith('New');

    act(() => {
      result.current.confirmRename();
      result.current.confirmRename();
    });
    await waitFor(() => expect(result.current.isEditing).toBe(false));

    expect(sentNames()).toEqual(['New']);
  });

  it('keeps the field open on the dropped confirm, so settling still closes it', async () => {
    const result = editingWith('New');

    act(() => {
      result.current.confirmRename();
      result.current.confirmRename();
    });

    expect(result.current.isEditing).toBe(true);
    await waitFor(() => expect(result.current.isEditing).toBe(false));
  });

  it('renames again once the in-flight rename settles', async () => {
    const result = editingWith('New');
    act(() => result.current.confirmRename());
    await waitFor(() => expect(result.current.isEditing).toBe(false));

    act(() => result.current.startEditing());
    act(() => result.current.setEditName('Newer'));
    act(() => result.current.confirmRename());
    await waitFor(() => expect(result.current.isEditing).toBe(false));

    expect(sentNames()).toEqual(['New', 'Newer']);
  });
});
