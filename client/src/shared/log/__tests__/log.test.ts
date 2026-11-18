import { consoleLogger, recordLogs, setLogger, warn } from '@shared/log';
import type { LoggedLine } from '@shared/log';

function sinkRecording(lines: LoggedLine[]): Pick<Console, 'warn'> {
  return {
    warn: (message: string, ...details: unknown[]) => {
      lines.push([message, ...details]);
    },
  };
}

describe('warn', () => {
  it('hands the message and every detail, unchanged, to the logger the app set', () => {
    const lines: LoggedLine[] = [];
    const cause = new Error('boom');
    setLogger(consoleLogger(sinkRecording(lines)));

    warn('[area] something failed', { status: 503 }, cause);

    expect(lines).toEqual([['[area] something failed', { status: 503 }, cause]]);
  });

  it('passes no detail the caller did not give', () => {
    const lines: LoggedLine[] = [];
    setLogger(consoleLogger(sinkRecording(lines)));

    warn('[area] bare message');

    expect(lines).toEqual([['[area] bare message']]);
  });

  it('stops reaching a logger once another is set', () => {
    const first = recordLogs();
    const second = recordLogs();

    warn('[area] after the swap');

    expect(first.warnings).toEqual([]);
    expect(second.warnings).toEqual([['[area] after the swap']]);
  });

  it('goes back to the console when set with no logger', () => {
    const logs = recordLogs();
    setLogger();

    expect(() => warn('[log] the console logger is back (expected in this test)')).not.toThrow();
    expect(logs.warnings).toEqual([]);
  });
});

describe('recordLogs', () => {
  it('spells out an error in its text, so a test can prove a secret never reached the log', () => {
    const logs = recordLogs();

    warn('[area] request failed', { status: 500 }, new TypeError('token abc123 rejected'));

    expect(logs.text()).toContain('TypeError: token abc123 rejected');
    expect(logs.text()).toContain('"status":500');
  });

  it('records nothing before anything warns', () => {
    expect(recordLogs().text()).toBe('[]');
  });
});
