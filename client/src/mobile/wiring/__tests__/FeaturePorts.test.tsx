import { renderHook } from '@testing-library/react-native';
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
import { useLibraryPort } from '@features/library';
import { createMemoryLibraryPort } from '@testing/fakes/library/inMemoryLibraryPort';
import { useSettingsPort } from '@testing/fakes/settings/useSettingsPort';
import { createMemorySettingsPort } from '@testing/fakes/settings/inMemorySettingsPort';
import { FeaturePorts, httpFeaturePorts, type FeaturePortSet } from '@/mobile';

function useEveryPort(): FeaturePortSet {
  return {
    acquisition: useAcquisitionPort(),
    detail: useDetailPort(),
    discover: useDiscoverPort(),
    favorites: useFavoritesPort(),
    library: useLibraryPort(),
    playlists: usePlaylistsPort(),
    settings: useSettingsPort(),
  };
}

function providing(ports: FeaturePortSet) {
  return function Wrapper({ children }: PropsWithChildren) {
    return <FeaturePorts ports={ports}>{children}</FeaturePorts>;
  };
}

describe('FeaturePorts', () => {
  it('hands every feature hook its HTTP adapter from the production port set', () => {
    const { result } = renderHook(useEveryPort, { wrapper: providing(httpFeaturePorts) });

    expect(result.current).toEqual(httpFeaturePorts);
    for (const key of Object.keys(httpFeaturePorts) as (keyof FeaturePortSet)[]) {
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

    for (const key of Object.keys(memory) as (keyof FeaturePortSet)[]) {
      expect(result.current[key]).toBe(memory[key]);
    }
  });

  it.each([
    ['acquisition', useAcquisitionPort],
    ['detail', useDetailPort],
    ['discover', useDiscoverPort],
    ['favorites', useFavoritesPort],
    ['library', useLibraryPort],
    ['playlists', usePlaylistsPort],
    ['settings', useSettingsPort],
  ])('throws for the %s port when nothing provides it', (name, usePort) => {
    expect(() => renderHook(() => usePort())).toThrow(`no ${name} port provided`);
  });
});
