import type { ReactElement, ReactNode } from 'react';

import { AcquisitionPortProvider } from '@shared/acquisition';
import { PlaylistsPortProvider } from '@shared/playlists';
import { DetailPortProvider, UnreleasedSearchHits } from '@features/detail';
import {
  DiscoverPortProvider,
  FavoritesPortProvider,
  UnreleasedHitsSlotProvider,
} from '@features/discovery';
import { LibraryPortProvider } from '@features/library';
import { SettingsProvider } from '@features/settings';

import type { FeaturePortSet } from '@/mobile/wiring/httpFeaturePorts';

interface FeaturePortsProps {
  ports: FeaturePortSet;
  children: ReactNode;
}

function DiscoveryPorts({ ports, children }: FeaturePortsProps): ReactElement {
  return (
    <DiscoverPortProvider port={ports.discover}>
      <FavoritesPortProvider port={ports.favorites}>
        <UnreleasedHitsSlotProvider view={UnreleasedSearchHits}>
          {children}
        </UnreleasedHitsSlotProvider>
      </FavoritesPortProvider>
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

function SharedPorts({ ports, children }: FeaturePortsProps): ReactElement {
  return (
    <AcquisitionPortProvider port={ports.acquisition}>
      <PlaylistsPortProvider port={ports.playlists}>{children}</PlaylistsPortProvider>
    </AcquisitionPortProvider>
  );
}

export function FeaturePorts({ ports, children }: FeaturePortsProps): ReactElement {
  return (
    <SharedPorts ports={ports}>
      <SettingsProvider port={ports.settings}>
        <CatalogPorts ports={ports}>
          <DiscoveryPorts ports={ports}>{children}</DiscoveryPorts>
        </CatalogPorts>
      </SettingsProvider>
    </SharedPorts>
  );
}
