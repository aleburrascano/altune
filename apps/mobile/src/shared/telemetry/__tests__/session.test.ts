import { advanceSession, SESSION_INACTIVITY_MS, type SessionState } from '../session';

describe('advanceSession', () => {
  const base: SessionState = { sessionId: 'sess-1', lastActivity: 1_000_000 };

  it('keeps the id within the inactivity window, advancing lastActivity', () => {
    const next = advanceSession(base, base.lastActivity + SESSION_INACTIVITY_MS - 1);
    expect(next.sessionId).toBe('sess-1');
    expect(next.lastActivity).toBe(base.lastActivity + SESSION_INACTIVITY_MS - 1);
  });

  it('rotates the id after 30 minutes of inactivity', () => {
    const now = base.lastActivity + SESSION_INACTIVITY_MS + 1;
    const next = advanceSession(base, now);
    expect(next.sessionId).not.toBe('sess-1');
    expect(next.lastActivity).toBe(now);
  });
});
