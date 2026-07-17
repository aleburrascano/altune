/**
 * SaveGlyph — the acquire lifecycle's state → glyph mapping, shared by the
 * hero save pill (TrackDetailBody) and the 40pt row control (TrackSaveControl)
 * so a new lifecycle state lands in one place. The two controls style their
 * `add` glyph differently (accent circle vs neutral pill), hence the two props.
 */

import type { ReactElement } from 'react';
import { ActivityIndicator } from 'react-native';

import { Check, Plus, RotateCw } from 'lucide-react-native';

import { useTheme } from '@shared/ui/theme';

import type { SaveControlState } from '../save-control-state';

export function SaveGlyph({
  state,
  addSize,
  addTone,
}: {
  state: SaveControlState;
  addSize: number;
  addTone: 'accent' | 'primary';
}): ReactElement {
  const theme = useTheme();
  if (state === 'saving') {
    return <ActivityIndicator size="small" color={theme.color.accent} />;
  }
  if (state === 'ready') {
    return <Check size={18} color={theme.color.success} />;
  }
  if (state === 'failed') {
    return <RotateCw size={17} color={theme.color.danger} />;
  }
  return (
    <Plus
      size={addSize}
      color={addTone === 'accent' ? theme.color.accent : theme.color.textPrimary}
    />
  );
}
