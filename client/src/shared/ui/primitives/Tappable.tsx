import type { Ref } from 'react';
import { Pressable } from 'react-native';
import type { GestureResponderEvent, PressableProps, View } from 'react-native';

import { recordTap } from '@shared/ui/primitives/recordTap';

export type TappableProps = Omit<PressableProps, 'onPress'> & {
  action: string;
  trackId?: string;
  ref?: Ref<View>;
  onPress: (event: GestureResponderEvent) => void;
};

export function Tappable({ action, trackId, onPress, disabled, ...rest }: TappableProps) {
  function handlePress(event: GestureResponderEvent) {
    if (disabled) {
      return;
    }
    recordTap(action, trackId);
    onPress(event);
  }
  return <Pressable {...rest} disabled={disabled} onPress={handlePress} />;
}
