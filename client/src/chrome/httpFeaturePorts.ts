import { httpDetailPort } from '@features/detail/adapters/http/httpDetailPort';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { httpDiscoverPort } from '@features/discovery/adapters/http/httpDiscoverPort';
import { httpFavoritesPort } from '@features/discovery/adapters/http/httpFavoritesPort';
import type { DiscoverPort } from '@features/discovery/ports/discoverPort';
import type { FavoritesPort } from '@features/discovery/ports/favoritesPort';
import { httpLibraryPort } from '@features/library/adapters/http/httpLibraryPort';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import { httpSettingsPort } from '@features/settings/adapters/http/httpSettingsPort';
import type { SettingsPort } from '@features/settings/ports/settingsPort';

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
