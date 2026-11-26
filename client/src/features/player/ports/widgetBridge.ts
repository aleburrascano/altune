export interface WidgetBridge {
  writeSnapshot(json: string): void;
  writeCoverFromUrl(url: string, fileName: string): Promise<void>;
  reloadWidget(): void;
}
