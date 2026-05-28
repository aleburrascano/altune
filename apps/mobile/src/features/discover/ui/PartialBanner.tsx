/**
 * PartialBanner — shown above results when any provider failed (ADR-0008).
 *
 * testID = "discover-partial-banner" (preserved). Sits as a SIBLING of the
 * results — it never replaces them, so partial responses still show data.
 */

import type { ReactElement } from 'react';

import { Banner, spacing } from '@shared/ui';

import type { DiscoveryProviderInfo } from '../../../shared/api-client/discovery';

export type PartialBannerProps = {
  providers: DiscoveryProviderInfo[];
};

export function PartialBanner({ providers }: PartialBannerProps): ReactElement {
  const unavailable = providers
    .filter((p) => p.status !== 'ok')
    .map((p) => p.provider)
    .join(', ');
  return (
    <Banner testID="discover-partial-banner" tone="warning" style={{ marginBottom: spacing.sm }}>
      {`Some sources are unavailable: ${unavailable}`}
    </Banner>
  );
}
