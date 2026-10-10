import { ArrowDownCircle, CircleAlert, CircleCheck, type LucideIcon } from 'lucide-react-native';

export type PinnedGlyphKind = 'downloaded' | 'downloading' | 'failed';

export const PINNED_GLYPH: Record<PinnedGlyphKind, LucideIcon> = {
  downloaded: CircleCheck,
  downloading: ArrowDownCircle,
  failed: CircleAlert,
};

export const PINNED_GLYPH_LABEL: Record<PinnedGlyphKind, string> = {
  downloaded: 'Downloaded',
  downloading: 'Downloading',
  failed: 'Download failed',
};
