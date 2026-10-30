import type { ChangeEvent, ReactElement } from 'react';

import { useWideWebLayout } from '@shared/ui/layout/useLayoutMode';
import { useTheme } from '@shared/ui/theme';

import { toVolume, usePlaybackVolumeStore } from '@features/playback/web/playbackVolume';

const VOLUME_RANGE = { min: 0, max: 1, step: 0.01 };
const VOLUME_WIDTH_PX = 120;

function setVolumeFromEvent(event: ChangeEvent<HTMLInputElement>): void {
  usePlaybackVolumeStore.getState().setVolume(toVolume(Number(event.currentTarget.value)));
}

function useVolumeProps() {
  const theme = useTheme();
  const volume = usePlaybackVolumeStore((s) => s.volume);
  return {
    ...VOLUME_RANGE,
    value: volume,
    onChange: setVolumeFromEvent,
    style: { width: VOLUME_WIDTH_PX, accentColor: theme.color.accent },
  };
}

function VolumeInput(): ReactElement {
  const props = useVolumeProps();
  return <input type="range" aria-label="Volume" {...props} />;
}

export function VolumeSlider(): ReactElement | null {
  const isWide = useWideWebLayout();
  return isWide ? <VolumeInput /> : null;
}
