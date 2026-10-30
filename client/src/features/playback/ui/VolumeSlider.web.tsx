import type { ReactElement } from 'react';

import { useWideWebLayout } from '@shared/ui/layout/useLayoutMode';
import { useTheme } from '@shared/ui/theme';

import { usePlaybackVolumeStore } from '@features/playback/web/playbackVolume';

export function VolumeSlider(): ReactElement | null {
  const theme = useTheme();
  const isWide = useWideWebLayout();
  const volume = usePlaybackVolumeStore((s) => s.volume);
  const setVolume = usePlaybackVolumeStore((s) => s.setVolume);

  if (!isWide) return null;
  return (
    <input
      type="range"
      aria-label="Volume"
      min={0}
      max={1}
      step={0.01}
      value={volume}
      onChange={(event) => setVolume(Number(event.currentTarget.value))}
      style={{ width: 120, accentColor: theme.color.accent }}
    />
  );
}
