import type { ReactNode } from 'react';
import { Repeat, Repeat1, Shuffle, SkipBack, SkipForward } from 'lucide-react-native';

import type { RepeatMode } from '@shared/playback/types';
import { IconButton } from '@shared/ui/primitives/IconButton';

export type TransportControlsProps = {
  shuffled: boolean;
  repeatMode: RepeatMode;
  hasNext: boolean;
  hasPrevious: boolean;
  canRestart: boolean;
  smallIconSize: number;
  largeIconSize: number;
  dimColor: string;
  activeColor: string;
  primaryColor: string;
  onToggleShuffle: () => void;
  onPrevious: () => void;
  onNext: () => void;
  onCycleRepeat: () => void;
  center: ReactNode;
};

function ShuffleControl({ shuffled, smallIconSize, dimColor, activeColor, onToggleShuffle }: TransportControlsProps) {
  const color = shuffled ? activeColor : dimColor;
  const label = shuffled ? 'Disable shuffle' : 'Enable shuffle';
  return <IconButton icon={Shuffle} size={smallIconSize} color={color} onPress={onToggleShuffle} accessibilityLabel={label} />;
}

function PreviousControl({ hasPrevious, canRestart, largeIconSize, dimColor, primaryColor, onPrevious }: TransportControlsProps) {
  const color = hasPrevious || canRestart ? primaryColor : dimColor;
  return <IconButton icon={SkipBack} size={largeIconSize} color={color} onPress={onPrevious} accessibilityLabel="Previous track" />;
}

function NextControl({ hasNext, largeIconSize, dimColor, primaryColor, onNext }: TransportControlsProps) {
  const color = hasNext ? primaryColor : dimColor;
  return <IconButton icon={SkipForward} size={largeIconSize} color={color} disabled={!hasNext} onPress={onNext} accessibilityLabel="Next track" />;
}

function RepeatControl({ repeatMode, smallIconSize, dimColor, activeColor, onCycleRepeat }: TransportControlsProps) {
  const icon = repeatMode === 'one' ? Repeat1 : Repeat;
  const color = repeatMode === 'off' ? dimColor : activeColor;
  return <IconButton icon={icon} size={smallIconSize} color={color} onPress={onCycleRepeat} accessibilityLabel={`Repeat: ${repeatMode}`} />;
}

function PlayRow(props: TransportControlsProps) {
  return (
    <>
      <PreviousControl {...props} />
      {props.center}
      <NextControl {...props} />
    </>
  );
}

export function TransportControls(props: TransportControlsProps) {
  return (
    <>
      <ShuffleControl {...props} />
      <PlayRow {...props} />
      <RepeatControl {...props} />
    </>
  );
}
