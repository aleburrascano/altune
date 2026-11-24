export type TabRoot = 'discover' | 'library';

export function tabRootFromSegments(segments: readonly string[]): TabRoot {
  return segments[1] === 'library' ? 'library' : 'discover';
}
