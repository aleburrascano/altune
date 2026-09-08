jest.mock('@supabase/supabase-js', () => ({
  createClient: (): { __fake: true } => ({ __fake: true }),
}));

const ORIGINAL_URL = process.env.EXPO_PUBLIC_SUPABASE_URL;
const ORIGINAL_ANON_KEY = process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY;

afterEach(() => {
  process.env.EXPO_PUBLIC_SUPABASE_URL = ORIGINAL_URL;
  process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY = ORIGINAL_ANON_KEY;
  jest.resetModules();
});

describe('supabaseClient — required configuration fails fast at app startup', () => {
  it.each([
    ['EXPO_PUBLIC_SUPABASE_URL', () => delete process.env.EXPO_PUBLIC_SUPABASE_URL],
    ['EXPO_PUBLIC_SUPABASE_ANON_KEY', () => delete process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY],
  ])('throws at import naming %s when it is unset', (name, unset) => {
    jest.resetModules();
    unset();

    expect(() => require('../supabaseClient')).toThrow(name);
  });

  it.each([
    ['EXPO_PUBLIC_SUPABASE_URL', (): void => void (process.env.EXPO_PUBLIC_SUPABASE_URL = '')],
    ['EXPO_PUBLIC_SUPABASE_ANON_KEY', (): void => void (process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY = '')],
  ])('throws at import naming %s when it is the empty string', (name, blank) => {
    jest.resetModules();
    blank();

    expect(() => require('../supabaseClient')).toThrow(name);
  });

  it('constructs without throwing when both variables are present', () => {
    jest.resetModules();
    process.env.EXPO_PUBLIC_SUPABASE_URL = 'https://fixture.supabase.co';
    process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY = 'fixture-anon-key';

    expect(() => require('../supabaseClient')).not.toThrow();
    const { supabase } = require('../supabaseClient') as { supabase: unknown };
    expect(supabase).toBeDefined();
  });
});
