import { MAX_MESSAGE_LENGTH, MIN_MESSAGE_LENGTH, isReportReady } from '../reportRules';

describe('isReportReady(): needs a kind and a trimmed message of at least MIN_MESSAGE_LENGTH', () => {
  it.each([
    ['no kind', null, '1234567890', false],
    ['kind, trimmed length one short', 'bug', ' 123456789 ', false],
    ['kind, trimmed length exactly the minimum', 'idea', ' 1234567890 ', true],
    ['kind, whitespace only', 'confusing', '              ', false],
  ] as const)('%s', (_label, kind, message, expected) => {
    expect(isReportReady(kind, message)).toBe(expected);
  });

  it('keeps the thresholds at 10 and 2000', () => {
    expect(MIN_MESSAGE_LENGTH).toBe(10);
    expect(MAX_MESSAGE_LENGTH).toBe(2000);
  });
});
