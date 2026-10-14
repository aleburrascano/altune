import { renderHook } from '@testing-library/react-native';
import type { PropsWithChildren } from 'react';

import { createMemoryDetailPort } from '@features/detail/__tests__/memoryDetailPort';
import { useDetailPort } from '@features/detail/ports/DetailPortProvider';
import { createMemoryDiscoverPort } from '@features/discovery/__tests__/memoryDiscoverPort';
import { createMemoryFavoritesPort } from '@features/discovery/__tests__/memoryFavoritesPort';
import { useDiscoverPort } from '@features/discovery/ports/DiscoverPortProvider';
import { useFavoritesPort } from '@features/discovery/ports/FavoritesPortProvider';
import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';
import { createMemorySettingsPort } from '@features/settings/__tests__/memorySettingsPort';
import { useSettingsPort } from '@features/settings/ports/SettingsPortProvider';

import { FeaturePorts } from '@/wiring/FeaturePorts';
import { httpFeaturePorts, type FeaturePortSet } from '@/wiring/httpFeaturePorts';

function useEveryPort(): FeaturePortSet {
  return {
    detail: useDetailPort(),
    discover: useDiscoverPort(),
    favorites: useFavoritesPort(),
    library: useLibraryPort(),
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
      detail: createMemoryDetailPort(),
      discover: createMemoryDiscoverPort(),
      favorites: createMemoryFavoritesPort(),
      library: createMemoryLibraryPort(),
      settings: createMemorySettingsPort(),
    };

    const { result } = renderHook(useEveryPort, { wrapper: providing(memory) });

    for (const key of Object.keys(memory) as (keyof FeaturePortSet)[]) {
      expect(result.current[key]).toBe(memory[key]);
    }
  });

  it.each([
    ['detail', useDetailPort],
    ['discover', useDiscoverPort],
    ['favorites', useFavoritesPort],
    ['library', useLibraryPort],
    ['settings', useSettingsPort],
  ])('throws for the %s port when nothing provides it', (name, usePort) => {
    jest.spyOn(console, 'error').mockImplementation(() => {});

    expect(() => renderHook(() => usePort())).toThrow(`no ${name} port provided`);
  });
});
