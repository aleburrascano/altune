import type { ReactElement, ReactNode } from 'react';
import { Alert } from 'react-native';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import * as api from '@shared/api-client/playlists';
import { ThemeProvider } from '@shared/ui/theme/ThemeProvider';

import { AddToPlaylistSheet } from '../AddToPlaylistSheet';

jest.mock('@shared/api-client/playlists');

const getPlaylists = api.getPlaylists as jest.MockedFunction<typeof api.getPlaylists>;
const addTracksToPlaylist = api.addTracksToPlaylist as jest.MockedFunction<
  typeof api.addTracksToPlaylist
>;

const clients: QueryClient[] = [];

function Wrap({ children }: { children: ReactNode }): ReactElement {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(client);
  return (
    <QueryClientProvider client={client}>
      <ThemeProvider>{children}</ThemeProvider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  jest.clearAllMocks();
  jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
  getPlaylists.mockResolvedValue({
    items: [
      {
        id: 'p1',
        name: 'Focus',
        track_count: 2,
        preview_artwork_urls: [],
        created_at: '',
        updated_at: '',
      },
    ],
    total: 1,
  });
  addTracksToPlaylist.mockResolvedValue({ added: 1, skipped: 0 });
});

afterEach(() => {
  while (clients.length > 0) {
    clients.pop()?.clear();
  }
});

it('resolves the track ids only when a playlist is picked, not when the sheet opens', async () => {
  const resolveTrackIds = jest.fn().mockResolvedValue(['t1']);
  render(
    <AddToPlaylistSheet
      visible
      label="Song Title"
      resolveTrackIds={resolveTrackIds}
      onClose={jest.fn()}
    />,
    { wrapper: Wrap },
  );

  await screen.findByTestId('add-to-playlist-p1');
  expect(resolveTrackIds).not.toHaveBeenCalled();

  fireEvent.press(screen.getByTestId('add-to-playlist-p1'));

  await waitFor(() => expect(resolveTrackIds).toHaveBeenCalledTimes(1));
});

it('adds the resolved ids, so a track saved on pick lands in the chosen playlist', async () => {
  const resolveTrackIds = jest.fn().mockResolvedValue(['saved-id']);
  render(
    <AddToPlaylistSheet
      visible
      label="Song Title"
      resolveTrackIds={resolveTrackIds}
      onClose={jest.fn()}
    />,
    { wrapper: Wrap },
  );

  fireEvent.press(await screen.findByTestId('add-to-playlist-p1'));

  await waitFor(() =>
    expect(addTracksToPlaylist).toHaveBeenCalledWith('p1', { track_ids: ['saved-id'] }),
  );
});

it('never calls the add endpoint when resolving the ids fails', async () => {
  const onClose = jest.fn();
  const resolveTrackIds = jest.fn().mockRejectedValue(new Error('save failed'));
  render(
    <AddToPlaylistSheet
      visible
      label="Song Title"
      resolveTrackIds={resolveTrackIds}
      onClose={onClose}
    />,
    { wrapper: Wrap },
  );

  fireEvent.press(await screen.findByTestId('add-to-playlist-p1'));

  await waitFor(() => expect(onClose).toHaveBeenCalled());
  expect(addTracksToPlaylist).not.toHaveBeenCalled();
});

it('never calls the add endpoint when nothing resolved', async () => {
  const resolveTrackIds = jest.fn().mockResolvedValue([]);
  render(
    <AddToPlaylistSheet
      visible
      label="Nothing"
      resolveTrackIds={resolveTrackIds}
      onClose={jest.fn()}
    />,
    { wrapper: Wrap },
  );

  fireEvent.press(await screen.findByTestId('add-to-playlist-p1'));

  await waitFor(() => expect(resolveTrackIds).toHaveBeenCalled());
  expect(addTracksToPlaylist).not.toHaveBeenCalled();
});

it('does not fetch the playlist list while hidden', () => {
  render(
    <AddToPlaylistSheet
      visible={false}
      label="Song Title"
      resolveTrackIds={jest.fn()}
      onClose={jest.fn()}
    />,
    { wrapper: Wrap },
  );

  expect(getPlaylists).not.toHaveBeenCalled();
});
