import { searchDiscovery } from '../discovery';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

function fullSearchResponse() {
  return {
    query: 'radiohead',
    query_norm: 'radiohead',
    results: [],
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 0,
    offset: 0,
    has_more: false,
  };
}

describe('searchDiscovery sends search_id so a later page reads the held slate', () => {
  it('omits search_id when none is given', async () => {
    __http.reply('GET /v1/discovery/search', { status: 200, json: fullSearchResponse() });

    await searchDiscovery({ q: 'radiohead', offset: 5 });

    const qp = new URLSearchParams(__http.last().query);
    expect(qp.has('search_id')).toBe(false);
  });

  it('forwards a given searchId as the search_id query param', async () => {
    __http.reply('GET /v1/discovery/search', { status: 200, json: fullSearchResponse() });

    await searchDiscovery({
      q: 'radiohead',
      offset: 20,
      searchId: '9f2c1b1e-2222-4444-8888-000000000001',
    });

    const qp = new URLSearchParams(__http.last().query);
    expect(qp.get('search_id')).toBe('9f2c1b1e-2222-4444-8888-000000000001');
    expect(qp.get('offset')).toBe('20');
  });
});
