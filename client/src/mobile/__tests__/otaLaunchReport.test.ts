import { UpdatesLogEntryCode, UpdatesLogEntryLevel, type UpdatesLogEntry } from 'expo-updates';

import { reportOtaLaunchFailures, type UpdateLog } from '@/mobile';

const crashOfNoUpdate: UpdatesLogEntry = {
  timestamp: 0,
  level: UpdatesLogEntryLevel.ERROR,
  code: UpdatesLogEntryCode.UPDATE_FAILED_TO_LOAD,
  message: 'JS bundle threw',
};
const crash: UpdatesLogEntry = { ...crashOfNoUpdate, updateId: '703a1adf' };

const DAY_MS = 86_400_000;
const LOG_WINDOW_DAYS = 7;

type FakeUpdateLog = UpdateLog & {
  readLogEntriesAsync: jest.Mock<Promise<UpdatesLogEntry[]>, [number?]>;
  clearLogEntriesAsync: jest.Mock<Promise<void>, []>;
};

function fakeUpdateLog(
  entries: UpdatesLogEntry[],
  launch: Partial<
    Pick<UpdateLog, 'isEnabled' | 'isEmergencyLaunch' | 'emergencyLaunchReason'>
  > = {},
): FakeUpdateLog {
  return {
    isEnabled: true,
    isEmergencyLaunch: false,
    emergencyLaunchReason: null,
    ...launch,
    readLogEntriesAsync: jest.fn(() => Promise.resolve(entries)),
    clearLogEntriesAsync: jest.fn(() => Promise.resolve()),
  };
}

const report = jest.fn();

beforeEach(() => {
  report.mockReset();
});

describe('reportOtaLaunchFailures', () => {
  it('reports each update error once, then clears the log', async () => {
    const updates = fakeUpdateLog([
      { ...crash, level: UpdatesLogEntryLevel.INFO },
      crash,
      { ...crashOfNoUpdate, level: UpdatesLogEntryLevel.FATAL },
    ]);
    await reportOtaLaunchFailures(updates, report);
    expect(report.mock.calls).toEqual([
      ['UpdateFailedToLoad 703a1adf: JS bundle threw'],
      ['UpdateFailedToLoad -: JS bundle threw'],
    ]);
    expect(updates.clearLogEntriesAsync).toHaveBeenCalled();
  });

  it('reports an emergency launch with or without its reason', async () => {
    const withReason = fakeUpdateLog([], {
      isEmergencyLaunch: true,
      emergencyLaunchReason: 'update crashed on launch',
    });
    const withoutReason = fakeUpdateLog([], { isEmergencyLaunch: true });
    await reportOtaLaunchFailures(withReason, report);
    await reportOtaLaunchFailures(withoutReason, report);
    expect(report.mock.calls).toEqual([
      ['emergency launch: update crashed on launch'],
      ['emergency launch: no reason'],
    ]);
    expect(withReason.clearLogEntriesAsync).not.toHaveBeenCalled();
    expect(withoutReason.clearLogEntriesAsync).not.toHaveBeenCalled();
  });

  it('stays quiet on a clean launch and when updates are off', async () => {
    const clean = fakeUpdateLog([{ ...crash, level: UpdatesLogEntryLevel.INFO }]);
    const off = fakeUpdateLog([crash], { isEnabled: false });
    await reportOtaLaunchFailures(clean, report);
    await reportOtaLaunchFailures(off, report);
    expect(report).not.toHaveBeenCalled();
    expect(off.readLogEntriesAsync).not.toHaveBeenCalled();
  });

  it('reads only the last week of the log and reports at most the five latest errors', async () => {
    const errors = ['1', '2', '3', '4', '5', '6'].map((n) => ({ ...crash, message: `crash ${n}` }));
    const updates = fakeUpdateLog(errors);
    await reportOtaLaunchFailures(updates, report);
    expect(updates.readLogEntriesAsync).toHaveBeenCalledWith(LOG_WINDOW_DAYS * DAY_MS);
    expect(report.mock.calls.map(([line]) => line)).toEqual(
      ['2', '3', '4', '5', '6'].map((n) => `UpdateFailedToLoad 703a1adf: crash ${n}`),
    );
  });

  it('records a failure as a telemetry event by default', async () => {
    const updates = fakeUpdateLog([crash]);
    await expect(reportOtaLaunchFailures(updates)).resolves.toBeUndefined();
    expect(updates.clearLogEntriesAsync).toHaveBeenCalled();
  });
});
