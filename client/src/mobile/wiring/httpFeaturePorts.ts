import { httpDetailPort, type DetailPort } from '@features/detail';
import {
  httpDiscoverPort,
  httpFavoritesPort,
  type DiscoverPort,
  type FavoritesPort,
} from '@features/discovery';
import { httpLibraryPort, type LibraryPort } from '@features/library';
import { httpSettingsPort, type SettingsPort } from '@features/settings';

export interface FeaturePortSet {
  detail: DetailPort;
  discover: DiscoverPort;
  favorites: FavoritesPort;
  library: LibraryPort;
  settings: SettingsPort;
}

export const httpFeaturePorts: FeaturePortSet = {
  detail: httpDetailPort,
  discover: httpDiscoverPort,
  favorites: httpFavoritesPort,
  library: httpLibraryPort,
  settings: httpSettingsPort,
};
