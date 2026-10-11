import { Pressable } from 'react-native';
import type { PressableProps } from 'react-native';

import { recordTap } from '@shared/ui/primitives/recordTap';

export type TappableProps = Omit<PressableProps, 'onPress'> & {
  action: string;
  trackId?: string;
  onPress: () => void;
};

export function Tappable({ action, trackId, onPress, disabled, ...rest }: TappableProps) {
  function handlePress() {
    if (disabled) {
      return;
    }
    recordTap(action, trackId);
    onPress();
  }
  return <Pressable {...rest} disabled={disabled} onPress={handlePress} />;
}
