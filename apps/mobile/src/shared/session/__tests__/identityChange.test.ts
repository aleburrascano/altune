import { hasSignedInUser, notifyIdentityChange, onIdentityChange } from '../signOutCleanup';

describe('identity change registry', () => {
  it('runs listeners in registration order, then sets the signed-in flag', () => {
    const calls: string[] = [];
    const stopFirst = onIdentityChange(() => calls.push(`first:${String(hasSignedInUser())}`));
    const stopSecond = onIdentityChange((userId) => calls.push(`second:${String(userId)}`));
    notifyIdentityChange('user-1');
    expect(calls).toEqual(['first:false', 'second:user-1']);
    expect(hasSignedInUser()).toBe(true);
    stopFirst();
    stopSecond();
    notifyIdentityChange(null);
    expect(calls).toHaveLength(2);
    expect(hasSignedInUser()).toBe(false);
  });
});
