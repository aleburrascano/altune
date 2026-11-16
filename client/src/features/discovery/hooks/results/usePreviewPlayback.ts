import type { DiscoveryResult } from '@shared/catalog';
import { usePlayback, getPreviewUrl } from '@shared/playback';
import type { PlaybackState } from '@shared/playback';
import { tapFeedback } from '@shared/ui';

type PreviewPlayback =
  | { readonly hasPreview: false }
  | {
      readonly hasPreview: true;
      readonly isPlaying: boolean;
      readonly togglePreview: () => void;
    };

function isPreviewPlaying(
  { track, status }: Pick<PlaybackState, 'track' | 'status'>,
  previewUrl: string,
): boolean {
  return (
    track?.source.kind === 'preview' &&
    track.source.previewUrl === previewUrl &&
    status === 'playing'
  );
}

export function usePreviewPlayback(
  result: DiscoveryResult,
  feedback: typeof tapFeedback = tapFeedback,
): PreviewPlayback {
  const { track, status, play, pause } = usePlayback();
  const previewUrl = result.kind === 'track' ? getPreviewUrl(result.extras) : null;

  if (previewUrl === null) return { hasPreview: false };

  const isPlaying = isPreviewPlaying({ track, status }, previewUrl);

  const togglePreview = (): void => {
    feedback('light');
    if (isPlaying) {
      pause();
      return;
    }
    void play({
      source: { kind: 'preview', previewUrl },
      title: result.title,
      artist: result.subtitle ?? '',
      artworkUrl: result.image_url,
    });
  };

  return { hasPreview: true, isPlaying, togglePreview };
}
