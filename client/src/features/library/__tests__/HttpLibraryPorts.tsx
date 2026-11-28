import type { ReactElement, ReactNode } from 'react';
import { AcquisitionPortProvider } from '@shared/acquisition';
import { PlaylistsPortProvider } from '@shared/playlists';
import { apiClientAcquisitionPort, apiClientPlaylistsPort } from '@testing/apiClientPorts';
import { httpLibraryPort } from '@features/library/adapters/http/libraryHttp';
import { LibraryProvider } from '@features/library/view/LibraryProvider';

type HttpLibraryPortsProps = { children: ReactNode };

export function HttpLibraryPorts({ children }: HttpLibraryPortsProps): ReactElement {
  return (
    <AcquisitionPortProvider port={apiClientAcquisitionPort}>
      <PlaylistsPortProvider port={apiClientPlaylistsPort}>
        <LibraryProvider port={httpLibraryPort}>{children}</LibraryProvider>
      </PlaylistsPortProvider>
    </AcquisitionPortProvider>
  );
}
