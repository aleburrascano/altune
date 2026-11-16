import { useState } from 'react';
import type { PressableStateCallbackType, StyleProp, ViewStyle } from 'react-native';

type InteractionState = {
  hovered: boolean;
  focused: boolean;
};

export type PressableInteractionState = PressableStateCallbackType & InteractionState;

type InteractionStyle = (state: PressableInteractionState) => StyleProp<ViewStyle>;

type PressableInteractionProps = {
  onHoverIn: () => void;
  onHoverOut: () => void;
  onFocus: () => void;
  onBlur: () => void;
  style: (state: PressableStateCallbackType) => StyleProp<ViewStyle>;
};

type Setter = (value: boolean) => void;

function interactionHandlers(setHovered: Setter, setFocused: Setter) {
  return {
    onHoverIn: () => setHovered(true),
    onHoverOut: () => setHovered(false),
    onFocus: () => setFocused(true),
    onBlur: () => setFocused(false),
  };
}

function styleWithInteraction(style: InteractionStyle, interaction: InteractionState) {
  return (state: PressableStateCallbackType) => style({ ...state, ...interaction });
}

export type PressableInteraction = InteractionState & {
  pressableProps: PressableInteractionProps;
};

type Setters = [Setter, Setter];

function pressablePropsFor(style: InteractionStyle, setters: Setters, state: InteractionState) {
  return {
    ...interactionHandlers(...setters),
    style: styleWithInteraction(style, state),
  };
}

export function usePressableInteraction(style: InteractionStyle): PressableInteraction {
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const state = { hovered, focused };
  return { ...state, pressableProps: pressablePropsFor(style, [setHovered, setFocused], state) };
}
