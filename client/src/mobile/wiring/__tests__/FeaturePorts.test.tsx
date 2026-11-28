import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, renderHook, screen } from '@testing-library/react-native';
import type { PropsWithChildren } from 'react';

import { useAcquisitionPort } from '@shared/acquisition';
import { createMemoryAcquisitionPort } from '@testing/fakes/acquisition/inMemoryAcquisitionPort';
import { usePlaylistsPort } from '@shared/playlists';
import { createMemoryPlaylistsPort } from '@testing/fakes/playlists/inMemoryPlaylistsPort';
import { useDetailPort } from '@features/detail';
import { createMemoryDetailPort } from '@testing/fakes/detail/inMemoryDetailPort';
import { useDiscoverPort, useFavoritesPort } from '@features/discovery';
import { createMemoryDiscoverPort } from '@testing/fakes/discovery/inMemoryDiscoverPort';
import { createMemoryFavoritesPort } from '@testing/fakes/discovery/inMemoryFavoritesPort';
import { SidebarPlaylists } from '@features/library';
import { asPlaylistId } from '@shared/catalog';
import type { Navigator } from '@shared/navigation';
import { createMemoryLibraryPort } from '@testing/fakes/library/inMemoryLibraryPort';
import { SettingsScreen } from '@features/settings';
import { createMemorySettingsPort } from '@testing/fakes/settings/inMemorySettingsPort';
import { signInTestUser } from '@testing/signInTestUser';
import { retryOptionsWithJitter } from '@testing/retryOptions';
import { FeaturePorts, httpFeaturePorts, type FeaturePortSet } from '@/mobile';

type HookPorts = Omit<FeaturePortSet, 'settings' | 'library'>;

const navigator: Navigator = {
  push: jest.fn(),
  replace: jest.fn(),
  back: jest.fn(),
  canGoBack: () => false,
};

function useEveryPort(): HookPorts {
  return {
    acquisition: useAcquisitionPort(),
    detail: useDetailPort(),
    discover: useDiscoverPort(),
    favorites: useFavoritesPort(),
    playlists: usePlaylistsPort(),
  };
}

function providing(ports: FeaturePortSet) {
  return function Wrapper({ children }: PropsWithChildren) {
    return <FeaturePorts ports={ports}>{children}</FeaturePorts>;
  };
}

function withoutScreenPorts({
  settings: _settings,
  library: _library,
  ...hookPorts
}: FeaturePortSet): HookPorts {
  return hookPorts;
}

describe('FeaturePorts', () => {
  it('hands every feature hook its HTTP adapter from the production port set', () => {
    const { result } = renderHook(useEveryPort, { wrapper: providing(httpFeaturePorts) });

    expect(result.current).toEqual(withoutScreenPorts(httpFeaturePorts));
    for (const key of Object.keys(result.current) as (keyof HookPorts)[]) {
      expect(result.current[key]).toBe(httpFeaturePorts[key]);
    }
  });

  it('hands every feature hook the in-memory port it was given', () => {
    const memory: FeaturePortSet = {
      acquisition: createMemoryAcquisitionPort({ trackIds: [] }).port,
      detail: createMemoryDetailPort(),
      discover: createMemoryDiscoverPort(),
      favorites: createMemoryFavoritesPort(),
      library: createMemoryLibraryPort(),
      playlists: createMemoryPlaylistsPort(),
      settings: createMemorySettingsPort(),
    };

    const { result } = renderHook(useEveryPort, { wrapper: providing(memory) });

    for (const key of Object.keys(result.current) as (keyof HookPorts)[]) {
      expect(result.current[key]).toBe(memory[key]);
    }
  });

  it('hands the settings screen the settings port it was given', async () => {
    await signInTestUser();
    const page = { scanned: 9, updated: 2, truncated: false, nextOffset: 0 };
    const memory = { ...httpFeaturePorts, settings: createMemorySettingsPort({ pages: [page] }) };

    const Ports = providing(memory);
    render(<SettingsScreen retryOptions={retryOptionsWithJitter()} />, {
      wrapper: ({ children }: PropsWithChildren) => (
        <QueryClientProvider client={new QueryClient()}>
          <Ports>{children}</Ports>
        </QueryClientProvider>
      ),
    });
    fireEvent.press(screen.getByTestId('settings-backfill-featured'));

    expect(await screen.findByText('Updated 2 of 9 tracks')).toBeTruthy();
  });

  it('hands the library sidebar the library port it was given', async () => {
    const playlist = {
      id: asPlaylistId('p1'),
      name: 'Late Night',
      track_count: 0,
      preview_artwork_urls: [],
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
      total_duration_seconds: 0,
      tracks: [],
    };
    const memory = {
      ...httpFeaturePorts,
      library: createMemoryLibraryPort({ playlists: [playlist] }),
      playlists: createMemoryPlaylistsPort(),
    };

    const Ports = providing(memory);
    render(<SidebarPlaylists navigator={navigator} />, {
      wrapper: ({ children }: PropsWithChildren) => (
        <QueryClientProvider client={new QueryClient()}>
          <Ports>{children}</Ports>
        </QueryClientProvider>
      ),
    });

    expect(await screen.findByText('Late Night')).toBeTruthy();
  });

  it('throws for the library port when nothing provides it', () => {
    expect(() =>
      render(<SidebarPlaylists navigator={navigator} />, {
        wrapper: ({ children }: PropsWithChildren) => (
          <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
        ),
      }),
    ).toThrow('no library port provided');
  });

  it.each([
    ['acquisition', useAcquisitionPort],
    ['detail', useDetailPort],
    ['discover', useDiscoverPort],
    ['favorites', useFavoritesPort],
    ['playlists', usePlaylistsPort],
  ])('throws for the %s port when nothing provides it', (name, usePort) => {
    expect(() => renderHook(() => usePort())).toThrow(`no ${name} port provided`);
  });

  it('throws for the settings port when nothing provides it', () => {
    expect(() =>
      render(<SettingsScreen retryOptions={retryOptionsWithJitter()} />, {
        wrapper: ({ children }: PropsWithChildren) => (
          <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
        ),
      }),
    ).toThrow('no settings port provided');
  });
});
