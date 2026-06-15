/**
 * Shared helpers and styles for detail screen sub-components.
 */

import { StyleSheet } from 'react-native';

import type { useQueryClient } from '@tanstack/react-query';
import type { InfiniteData } from '@tanstack/react-query';
import type { ListTracksResponse } from '@shared/api-client/types';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { PlaybackContextValue, PlaybackSource } from '@shared/playback/types';

import { spacing } from '@shared/ui/theme/tokens';

export function _albumYear(album: DiscoveryResult): string | null {
  const releaseDate = album.extras['release_date'];
  if (typeof releaseDate === 'string') return releaseDate.slice(0, 4);
  const yearExtra = album.extras['year'];
  if (typeof yearExtra === 'string' || typeof yearExtra === 'number') return String(yearExtra);
  return null;
}

export function _isTrackInLibraryCache(
  queryClient: ReturnType<typeof useQueryClient>,
  title: string,
  artist: string | null,
): boolean {
  const homeData = queryClient.getQueryData<ListTracksResponse>(['library-home']);
  if (homeData) {
    const normalTitle = title.toLowerCase().trim();
    const normalArtist = (artist ?? '').toLowerCase().trim();
    return homeData.items.some(
      (t) => t.title.toLowerCase().trim() === normalTitle && t.artist.toLowerCase().trim() === normalArtist,
    );
  }
  const infiniteData = queryClient.getQueryData<InfiniteData<ListTracksResponse>>(['library']);
  if (!infiniteData) return false;
  const normalTitle = title.toLowerCase().trim();
  const normalArtist = (artist ?? '').toLowerCase().trim();
  return infiniteData.pages.some((page) =>
    page.items.some(
      (t) => t.title.toLowerCase().trim() === normalTitle && t.artist.toLowerCase().trim() === normalArtist,
    ),
  );
}

export function isCurrentlyPlaying(
  playback: Pick<PlaybackContextValue, 'status' | 'track'>,
  source: PlaybackSource,
): boolean {
  if (playback.status !== 'playing' || !playback.track) return false;
  if (source.kind === 'library') {
    return playback.track.source.kind === 'library' && playback.track.source.trackId === source.trackId;
  }
  return playback.track.source.kind === 'preview' && playback.track.source.previewUrl === source.previewUrl;
}

/** Styles shared across multiple detail sub-components. */
export const sharedStyles = StyleSheet.create({
  trackRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.md,
    gap: spacing.md,
    minHeight: 48,
  },
  trackInfo: { flex: 1 },
  retryButton: { marginTop: spacing.sm },
  sectionTitle: { marginBottom: spacing.sm },
  albumsSection: { marginTop: spacing.xl },
});
