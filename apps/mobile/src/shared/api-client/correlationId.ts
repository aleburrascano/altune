import { Platform } from 'react-native';

/** The header the Go API reads, stamps into its log lines and echoes back. */
export const CORRELATION_HEADER = 'X-Correlation-ID';

const ID_BYTES = 8;

/**
 * A fresh id for one outgoing request, or null where it must not be sent.
 * The server accepts up to 64 chars of [A-Za-z0-9_-]; 16 hex chars fit. Not a
 * secret, so Math.random is enough.
 *
 * Web gets null: the API's CORS policy does not list X-Correlation-ID as an
 * allowed header, so a browser preflight would reject every request carrying
 * it. Native clients do no preflight.
 */
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
