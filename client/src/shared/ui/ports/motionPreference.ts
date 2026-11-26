export interface MotionPreference {
  isReduced(): Promise<boolean>;
  onChange(listener: (reduced: boolean) => void): () => void;
}
