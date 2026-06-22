/**
 * DiscogsEnrichmentSection — Discogs liner-notes block on album detail.
 *
 * Renders the metadata Discogs uniquely carries: style pills, personnel credits
 * grouped by role, label + catalog, format/country, and the community
 * demand/rating signal. Album-scoped — rendered below the tracklist. Renders
 * nothing when there is no enrichment (docs/providers/discogs.md caps 3–6).
 */

import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme/useTheme';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type {
  DiscogsCredit,
  DiscogsEnrichmentResponse,
} from '@shared/api-client/discovery';

const MAX_STYLES = 6;
const MAX_CREDIT_ROLES = 8;
const MAX_NAMES_PER_ROLE = 6;

export type CreditGroup = { role: string; names: string[] };

// groupCreditsByRole collapses a flat credit list into ordered role groups,
// preserving first-seen order of both roles and names. Pure — unit-tested.
export function groupCreditsByRole(credits: DiscogsCredit[]): CreditGroup[] {
  const order: string[] = [];
  const byRole = new Map<string, string[]>();
  for (const { role, name } of credits) {
    if (!byRole.has(role)) {
      byRole.set(role, []);
      order.push(role);
    }
    const names = byRole.get(role)!;
    if (!names.includes(name)) {
      names.push(name);
    }
  }
  return order.map((role) => ({ role, names: byRole.get(role)! }));
}

function communityLine(c: DiscogsEnrichmentResponse['community']): string | null {
  const parts: string[] = [];
  if (c.have > 0) parts.push(`${c.have.toLocaleString()} have`);
  if (c.want > 0) parts.push(`${c.want.toLocaleString()} want`);
  if (c.rating > 0) parts.push(`★ ${c.rating.toFixed(2)} (${c.votes})`);
  return parts.length > 0 ? parts.join('  ·  ') : null;
}

function detailLine(e: DiscogsEnrichmentResponse): string | null {
  const parts = [
    ...e.formats.slice(0, 2),
    e.country !== '' ? e.country : null,
    e.year > 0 ? String(e.year) : null,
  ].filter((p): p is string => p !== null && p !== '');
  return parts.length > 0 ? parts.join('  ·  ') : null;
}

export function DiscogsEnrichmentSection({
  enrichment,
}: {
  enrichment: DiscogsEnrichmentResponse | null;
}): ReactElement | null {
  const theme = useTheme();

  if (enrichment === null) {
    return null;
  }

  const styles_ = enrichment.styles.slice(0, MAX_STYLES);
  const creditGroups = groupCreditsByRole(enrichment.credits).slice(0, MAX_CREDIT_ROLES);
  const primaryLabel = enrichment.labels[0] ?? null;
  const community = communityLine(enrichment.community);
  const details = detailLine(enrichment);

  return (
    <View testID="detail-discogs" style={styles.section}>
      <Text variant="label" tone="tertiary" style={styles.heading}>
        Discogs
      </Text>

      {styles_.length > 0 ? (
        <View style={styles.chips}>
          {styles_.map((style, index) => (
            <View
              key={style}
              testID={`detail-discogs-style-${index}`}
              style={[styles.chip, { borderColor: theme.color.border }]}
            >
              <Text variant="caption" tone="secondary">
                {style}
              </Text>
            </View>
          ))}
        </View>
      ) : null}

      {creditGroups.length > 0 ? (
        <View style={styles.credits}>
          {creditGroups.map((group, index) => (
            <View key={group.role} testID={`detail-discogs-credit-${index}`} style={styles.creditRow}>
              <Text variant="caption" tone="tertiary" style={styles.creditRole}>
                {group.role}
              </Text>
              <Text variant="caption" tone="secondary" style={styles.creditNames}>
                {group.names.slice(0, MAX_NAMES_PER_ROLE).join(', ')}
              </Text>
            </View>
          ))}
        </View>
      ) : null}

      {primaryLabel !== null ? (
        <Text testID="detail-discogs-label" variant="caption" tone="tertiary" style={styles.line}>
          {primaryLabel.catno !== ''
            ? `${primaryLabel.name} — ${primaryLabel.catno}`
            : primaryLabel.name}
        </Text>
      ) : null}

      {details !== null ? (
        <Text variant="caption" tone="tertiary" style={styles.line}>
          {details}
        </Text>
      ) : null}

      {community !== null ? (
        <Text testID="detail-discogs-community" variant="caption" tone="tertiary" style={styles.line}>
          {community}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  section: { marginTop: spacing.xl, gap: spacing.sm },
  heading: { textAlign: 'center', textTransform: 'uppercase', letterSpacing: 1 },
  chips: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    justifyContent: 'center',
    gap: spacing.sm,
  },
  chip: {
    borderWidth: 1,
    borderRadius: radius.full,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
  },
  credits: { gap: spacing.xs, marginTop: spacing.sm },
  creditRow: { flexDirection: 'row', gap: spacing.sm },
  creditRole: { flex: 0.4 },
  creditNames: { flex: 0.6 },
  line: { textAlign: 'center' },
});
