import type { ReactElement, ReactNode } from 'react';
import { AcquisitionPortProvider } from '@shared/acquisition';
import { PlaylistsPortProvider } from '@shared/playlists';
import { apiClientAcquisitionPort, apiClientPlaylistsPort } from '@testing/apiClientPorts';
import { httpLibraryPort, LibraryPortProvider } from '@features/library';

type HttpLibraryPortsProps = { children: ReactNode };

export function HttpLibraryPorts({ children }: HttpLibraryPortsProps): ReactElement {
  return (
    <AcquisitionPortProvider port={apiClientAcquisitionPort}>
      <PlaylistsPortProvider port={apiClientPlaylistsPort}>
        <LibraryPortProvider port={httpLibraryPort}>{children}</LibraryPortProvider>
      </PlaylistsPortProvider>
    </AcquisitionPortProvider>
  );
}
