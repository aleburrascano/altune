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

function shuffleLook(p: TransportControlsProps) {
  return {
    size: p.smallIconSize,
    color: p.shuffled ? p.activeColor : p.dimColor,
    onPress: p.onToggleShuffle,
    accessibilityLabel: p.shuffled ? 'Disable shuffle' : 'Enable shuffle',
  };
}

function ShuffleControl(props: TransportControlsProps) {
  return <IconButton action="playback.transport.shuffle" icon={Shuffle} {...shuffleLook(props)} />;
}

function previousLook(p: TransportControlsProps) {
  return {
    size: p.largeIconSize,
    color: p.hasPrevious || p.canRestart ? p.primaryColor : p.dimColor,
    onPress: p.onPrevious,
    accessibilityLabel: 'Previous track',
  };
}

function PreviousControl(props: TransportControlsProps) {
  return (
    <IconButton action="playback.transport.previous" icon={SkipBack} {...previousLook(props)} />
  );
}

function nextLook(p: TransportControlsProps) {
  return {
    size: p.largeIconSize,
    color: p.hasNext ? p.primaryColor : p.dimColor,
    disabled: !p.hasNext,
    onPress: p.onNext,
    accessibilityLabel: 'Next track',
  };
}

function NextControl(props: TransportControlsProps) {
  return <IconButton action="playback.transport.next" icon={SkipForward} {...nextLook(props)} />;
}

function repeatLook(p: TransportControlsProps) {
  return {
    icon: p.repeatMode === 'one' ? Repeat1 : Repeat,
    size: p.smallIconSize,
    color: p.repeatMode === 'off' ? p.dimColor : p.activeColor,
    onPress: p.onCycleRepeat,
    accessibilityLabel: `Repeat: ${p.repeatMode}`,
  };
}

function RepeatControl(props: TransportControlsProps) {
  return <IconButton action="playback.transport.repeat" {...repeatLook(props)} />;
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
