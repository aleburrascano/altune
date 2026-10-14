import type { ReactElement, ReactNode } from 'react';

import { DetailPortProvider } from '@features/detail/ports/DetailPortProvider';
import { DiscoverPortProvider } from '@features/discovery/ports/DiscoverPortProvider';
import { FavoritesPortProvider } from '@features/discovery/ports/FavoritesPortProvider';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';
import { SettingsPortProvider } from '@features/settings/ports/SettingsPortProvider';

import type { FeaturePortSet } from '@/wiring/httpFeaturePorts';

interface FeaturePortsProps {
  ports: FeaturePortSet;
  children: ReactNode;
}

function DiscoveryPorts({ ports, children }: FeaturePortsProps): ReactElement {
  return (
    <DiscoverPortProvider port={ports.discover}>
      <FavoritesPortProvider port={ports.favorites}>{children}</FavoritesPortProvider>
    </DiscoverPortProvider>
  );
}

function CatalogPorts({ ports, children }: FeaturePortsProps): ReactElement {
  return (
    <DetailPortProvider port={ports.detail}>
      <LibraryPortProvider port={ports.library}>{children}</LibraryPortProvider>
    </DetailPortProvider>
  );
}

export function FeaturePorts({ ports, children }: FeaturePortsProps): ReactElement {
  return (
    <SettingsPortProvider port={ports.settings}>
      <CatalogPorts ports={ports}>
        <DiscoveryPorts ports={ports}>{children}</DiscoveryPorts>
      </CatalogPorts>
    </SettingsPortProvider>
  );
}
