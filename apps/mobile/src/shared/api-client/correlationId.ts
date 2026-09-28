import { Platform } from 'react-native';

export const CORRELATION_HEADER = 'X-Correlation-ID';

const ID_BYTES = 8;

export function newCorrelationId(): string | null {
  if (Platform.OS === 'web') return null;
  let id = '';
  for (let i = 0; i < ID_BYTES; i += 1) {
    id += Math.floor(Math.random() * 256)
      .toString(16)
      .padStart(2, '0');
  }
  return id;
}
