import type { InfiniteData } from '@tanstack/react-query';

import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

export type SearchPageParam = { offset: number; searchId: string | undefined };

export type SearchPages = InfiniteData<DiscoverySearchResponse, SearchPageParam>;
