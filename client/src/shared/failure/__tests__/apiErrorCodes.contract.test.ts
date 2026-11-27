import { API_ERROR_CODES } from '@shared/failure';
import { wireFixture } from '@testing/wireFixtures';

describe('the ApiErrorCode union against the catalog the Go server wrote to its fixture', () => {
  it('lists exactly the codes the server can send', () => {
    const serverCodes = wireFixture('api_error_codes') as string[];

    expect(serverCodes.length).toBeGreaterThan(0);
    expect([...API_ERROR_CODES].sort()).toEqual([...serverCodes].sort());
  });

  it('has no duplicate code', () => {
    expect(new Set(API_ERROR_CODES).size).toBe(API_ERROR_CODES.length);
  });
});
