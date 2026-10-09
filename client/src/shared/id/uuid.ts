import * as Crypto from 'expo-crypto';

export function randomUuid(): string {
  return Crypto.randomUUID();
}
