import { useState } from 'react';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { albumExtras } from '../extras-accessors';

const SECTION_CAP = 10;

export const RECORD_TYPES: readonly { type: string; label: string }[] = [
  { type: 'album', label: 'Albums' },
  { type: 'single', label: 'Singles' },
  { type: 'ep', label: 'EPs' },
];

export type RecordTypeGroup = { type: string; label: string; count: number };

export type DiscographyFilter = {
  present: readonly RecordTypeGroup[];
  active: RecordTypeGroup;
  items: DiscoveryResult[];
  capped: DiscoveryResult[];
  hasMore: boolean;
  select: (type: string) => void;
  expand: () => void;
};

function bucketFor(album: DiscoveryResult): string {
  const type = albumExtras(album.extras).recordType?.toLowerCase() ?? 'album';
  return type === 'compilation' ? 'album' : type;
}

function groupByRecordType(albums: DiscoveryResult[]): Map<string, DiscoveryResult[]> {
  const grouped = new Map<string, DiscoveryResult[]>();
  for (const album of albums) {
    const bucket = bucketFor(album);
    const list = grouped.get(bucket);
    if (list) list.push(album);
    else grouped.set(bucket, [album]);
  }
  return grouped;
}

function presentGroups(grouped: Map<string, DiscoveryResult[]>): RecordTypeGroup[] {
  return RECORD_TYPES.filter((t) => (grouped.get(t.type)?.length ?? 0) > 0).map((t) => ({
    ...t,
    count: grouped.get(t.type)?.length ?? 0,
  }));
}

function activeGroupOf(present: RecordTypeGroup[], selected: string | null): RecordTypeGroup {
  return present.find((t) => t.type === selected) ?? present[0]!;
}

function windowOf(items: DiscoveryResult[], expanded: boolean): { capped: DiscoveryResult[]; hasMore: boolean } {
  const capped = expanded ? items : items.slice(0, SECTION_CAP);
  const hasMore = !expanded && items.length > SECTION_CAP;
  return { capped, hasMore };
}

type DerivedFilter = Omit<DiscographyFilter, 'select' | 'expand'>;

function deriveFilter(albums: DiscoveryResult[], selected: string | null, expanded: boolean): DerivedFilter | null {
  const grouped = groupByRecordType(albums);
  const present = presentGroups(grouped);
  if (present.length === 0) return null;
  const active = activeGroupOf(present, selected);
  const items = grouped.get(active.type) ?? [];
  return { present, active, items, ...windowOf(items, expanded) };
}

function selectHandler(
  setSelected: (type: string) => void,
  setExpanded: (expanded: boolean) => void,
): (type: string) => void {
  return (type) => {
    setSelected(type);
    setExpanded(false);
  };
}

export function useDiscographyFilter(albums: DiscoveryResult[]): DiscographyFilter | null {
  const [expanded, setExpanded] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const derived = deriveFilter(albums, selected, expanded);
  if (derived === null) return null;
  return { ...derived, select: selectHandler(setSelected, setExpanded), expand: () => setExpanded(true) };
}

export { SECTION_CAP };
