import type { ReactElement } from 'react';
import { Heart } from 'lucide-react-native';
import * as Haptics from 'expo-haptics';

import type { FavoriteTarget } from '@shared/api-client/favorites';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { useTheme } from '@shared/ui';

import { useFavorites } from '../useFavorites';

export type FavoriteButtonProps = {
  target: FavoriteTarget;
  testID?: string;
  size?: number;
};

export function FavoriteButton({ target, testID, size = 18 }: FavoriteButtonProps): ReactElement {
  const theme = useTheme();
  const favorites = useFavorites();
  const saved = favorites.isFavorite(target);

  return (
    <IconButton
      {...(testID != null ? { testID } : {})}
      icon={Heart}
      size={size}
      color={saved ? theme.color.accent : theme.color.textTertiary}
      onPress={() => {
        void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
        favorites.toggle(target);
      }}
      accessibilityLabel={saved ? `Unfavorite ${target.title}` : `Favorite ${target.title}`}
    />
  );
}
