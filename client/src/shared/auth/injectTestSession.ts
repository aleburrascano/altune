import type { AuthChangeEvent, Session } from '@supabase/supabase-js';

import { isTestAuthEnabled } from '@shared/auth/testAuthBoot';
import { supabase } from '@shared/auth/supabaseClient';

type SessionStore = {
  storageKey: string;
  storage: { setItem: (key: string, value: string) => Promise<void> };
  stopAutoRefresh?: () => unknown;
  _notifyAllSubscribers?: (event: AuthChangeEvent, session: Session) => Promise<void>;
};

function assertSessionStoreShape(store: SessionStore): void {
  const hasKey = typeof store.storageKey === 'string' && store.storageKey.length > 0;
  const hasStorage = typeof store.storage?.setItem === 'function';
  if (!hasKey || !hasStorage) {
    throw new Error(
      'test-auth: Supabase auth session-store internals changed ' +
        '(expected string `storageKey` and `storage.setItem`); the injection ' +
        'path must be updated for this @supabase/supabase-js version',
    );
  }
}

export async function injectTestSession(session: Session): Promise<void> {
  if (!isTestAuthEnabled()) {
    throw new Error(
      'test-auth is disabled: available only in a non-production build with EXPO_PUBLIC_TEST_AUTH=1',
    );
  }
  const store = supabase.auth as unknown as SessionStore;
  assertSessionStoreShape(store);
  await store.storage.setItem(store.storageKey, JSON.stringify(session));
  if (typeof store.stopAutoRefresh === 'function') {
    void store.stopAutoRefresh();
  }
  if (typeof store._notifyAllSubscribers === 'function') {
    await store._notifyAllSubscribers('SIGNED_IN', session);
  }
}
