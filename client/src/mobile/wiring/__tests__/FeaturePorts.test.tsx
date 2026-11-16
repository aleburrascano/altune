import { renderHook } from '@testing-library/react-native';
import type { PropsWithChildren } from 'react';

import { createMemoryAcquisitionPort, useAcquisitionPort } from '@shared/acquisition';
import { createMemoryPlaylistsPort, usePlaylistsPort } from '@shared/playlists';
import { createMemoryDetailPort, useDetailPort } from '@features/detail';
import {
  createMemoryDiscoverPort,
  createMemoryFavoritesPort,
  useDiscoverPort,
  useFavoritesPort,
} from '@features/discovery';
import { createMemoryLibraryPort, useLibraryPort } from '@features/library';
import { createMemorySettingsPort, useSettingsPort } from '@features/settings';
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
    jest.spyOn(console, 'error').mockImplementation(() => {});

    expect(() => renderHook(() => usePort())).toThrow(`no ${name} port provided`);
  });
});
