export type TapKind = 'light' | 'medium' | 'selection';

export type TapFeedback = (kind?: TapKind) => void;

export interface TapDevice {
  impact: (style: 'light' | 'medium') => void;
  selection: () => void;
}
