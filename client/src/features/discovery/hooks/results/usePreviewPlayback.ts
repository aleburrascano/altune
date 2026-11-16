import type { DiscoveryResult } from '@shared/catalog';
import { usePlayback, getPreviewUrl } from '@shared/playback';
import type { PlaybackState } from '@shared/playback';
import { tapFeedback } from '@shared/ui';

export type PreviewPlayback =
  | { readonly hasPreview: false }
  | {
      readonly hasPreview: true;
      readonly isPlaying: boolean;
      readonly togglePreview: () => void;
    };

type Playback = ReturnType<typeof usePlayback>;

type PreviewToggle = Pick<Playback, 'play' | 'pause'> & {
  isPlaying: boolean;
  feedback: typeof tapFeedback;
  item: DiscoveryResult;
  previewUrl: string;
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

function previewUrlOf(item: DiscoveryResult): string | null {
  return item.kind === 'track' ? getPreviewUrl(item.extras) : null;
}

function previewTrack(item: DiscoveryResult, previewUrl: string): Parameters<Playback['play']>[0] {
  return {
    source: { kind: 'preview', previewUrl },
    title: item.title,
    artist: item.subtitle ?? '',
    artworkUrl: item.image_url,
  };
}

function togglePreview(toggle: PreviewToggle): void {
  toggle.feedback('light');
  if (toggle.isPlaying) {
    toggle.pause();
    return;
  }
  void toggle.play(previewTrack(toggle.item, toggle.previewUrl));
}

function previewToggle(toggle: PreviewToggle): () => void {
  return () => togglePreview(toggle);
}

export function usePreviewPlayback(item: DiscoveryResult, feedback = tapFeedback): PreviewPlayback {
  const { track, status, play, pause } = usePlayback();
  const previewUrl = previewUrlOf(item);
  if (previewUrl === null) return { hasPreview: false };
  const isPlaying = isPreviewPlaying({ track, status }, previewUrl);
  const toggle = previewToggle({ isPlaying, play, pause, feedback, item, previewUrl });
  return { hasPreview: true, isPlaying, togglePreview: toggle };
}
