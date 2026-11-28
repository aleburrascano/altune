import type { AuthIntentResult } from '@features/signIn/core/authIntent';

export type CredentialConsumption = { credential: string; outcome: Promise<AuthIntentResult> };

export interface ConsumedCredentialStore {
  active(): CredentialConsumption | null;
  claim(consumption: CredentialConsumption): void;
  releaseIfActive(consumption: CredentialConsumption): void;
}

export function createConsumedCredentialStore(): ConsumedCredentialStore {
  let active: CredentialConsumption | null = null;
  return {
    active: () => active,
    claim: (consumption) => {
      active = consumption;
    },
    releaseIfActive: (consumption) => {
      if (active === consumption) active = null;
    },
  };
}
