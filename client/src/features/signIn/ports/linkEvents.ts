export interface LinkEvents {
  initialUrl(): Promise<string | null>;
  subscribeUrl(listener: (url: string) => void): () => void;
}
