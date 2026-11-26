export interface ScreenReader {
  isEnabled: () => Promise<boolean>;
  speak: (message: string) => void;
}

export function createAnnounce(reader: ScreenReader): (message: string) => void {
  return (message) => {
    if (message.length === 0) return;
    void reader.isEnabled().then((enabled) => {
      if (enabled) reader.speak(message);
    });
  };
}
