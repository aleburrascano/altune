/**
 * DiscogsArtistSection — Discogs artist metadata block on artist detail.
 *
 * Renders the biography, name history (real name + aliases), group/member
 * relationships, and tappable external links Discogs carries. Renders nothing
 * when there is no enrichment (docs/providers/discogs.md cap 7).
 */

import type { ReactElement } from 'react';
import { Linking, Pressable, StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme/useTheme';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type { DiscogsArtistEnrichmentResponse } from '@shared/api-client/discovery';

const MAX_LINKS = 8;

function joinList(values: string[], max: number): string | null {
  const trimmed = values.slice(0, max);
  return trimmed.length > 0 ? trimmed.join(', ') : null;
}

export function DiscogsArtistSection({
  enrichment,
}: {
  enrichment: DiscogsArtistEnrichmentResponse | null;
}): ReactElement | null {
  const theme = useTheme();

  if (enrichment === null) {
    return null;
  }

  const aka = joinList(enrichment.aliases, 6);
  const groups = joinList(enrichment.groups, 6);
  const members = joinList(enrichment.members, 8);
  const links = enrichment.links.slice(0, MAX_LINKS);

  return (
    <View testID="detail-discogs-artist" style={styles.section}>
      <Text variant="label" tone="tertiary" style={styles.heading}>
        Discogs
      </Text>

      {enrichment.profile !== '' ? (
        <Text testID="detail-discogs-bio" variant="body" tone="secondary" style={styles.bio}>
          {enrichment.profile}
        </Text>
      ) : null}

      {enrichment.real_name !== '' ? (
        <Text variant="caption" tone="tertiary" style={styles.line}>
          Real name: {enrichment.real_name}
        </Text>
      ) : null}

      {aka !== null ? (
        <Text variant="caption" tone="tertiary" style={styles.line}>
          Also known as: {aka}
        </Text>
      ) : null}

      {groups !== null ? (
        <Text testID="detail-discogs-groups" variant="caption" tone="tertiary" style={styles.line}>
          Member of: {groups}
        </Text>
      ) : null}

      {members !== null ? (
        <Text variant="caption" tone="tertiary" style={styles.line}>
          Members: {members}
        </Text>
      ) : null}

      {links.length > 0 ? (
        <View style={styles.links}>
          {links.map((link, index) => (
            <Pressable
              key={link.url}
              testID={`detail-discogs-link-${index}`}
              onPress={() => {
                void Linking.openURL(link.url);
              }}
              accessibilityRole="link"
              accessibilityLabel={`Open ${link.label}`}
              accessibilityHint="Opens an external page"
              style={({ pressed }) => [
                styles.chip,
                { borderColor: theme.color.border },
                pressed ? { opacity: 0.6 } : null,
              ]}
            >
              <Text variant="caption" tone="accent">
                {link.label}
              </Text>
            </Pressable>
          ))}
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  section: { marginTop: spacing.xl, gap: spacing.sm },
  heading: { textAlign: 'center', textTransform: 'uppercase', letterSpacing: 1 },
  bio: { textAlign: 'center' },
  line: { textAlign: 'center' },
  links: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    justifyContent: 'center',
    gap: spacing.sm,
    marginTop: spacing.sm,
  },
  chip: {
    borderWidth: 1,
    borderRadius: radius.full,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
  },
});
