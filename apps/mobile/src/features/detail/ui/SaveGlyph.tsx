import type { ReactElement } from 'react';
import { ActivityIndicator } from 'react-native';

import { Ban, Check, Plus, RotateCw } from 'lucide-react-native';

import { useTheme } from '@shared/ui/theme';

import type { SaveControlState } from '../save-control-state';

export function SaveGlyph({
  state,
  addSize,
}: {
  state: SaveControlState;
  addSize: number;
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
  if (state === 'rejected') {
    return <Ban size={17} color={theme.color.danger} />;
  }
  return <Plus size={addSize} color={theme.color.accent} />;
}
