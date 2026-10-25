import { reportOtaLaunchFailures } from '@shared/telemetry/native/otaLaunchReport';

const mockUpdates = {
  isEnabled: true,
  isEmergencyLaunch: false,
  emergencyLaunchReason: null as string | null,
  readLogEntriesAsync: jest.fn(),
  clearLogEntriesAsync: jest.fn(() => Promise.resolve()),
};
jest.mock('expo-updates', () => ({
  get isEnabled() {
    return mockUpdates.isEnabled;
  },
  get isEmergencyLaunch() {
    return mockUpdates.isEmergencyLaunch;
  },
  get emergencyLaunchReason() {
    return mockUpdates.emergencyLaunchReason;
  },
  readLogEntriesAsync: (maxAge: number) => mockUpdates.readLogEntriesAsync(maxAge),
  clearLogEntriesAsync: () => mockUpdates.clearLogEntriesAsync(),
}));

const crash = {
  level: 'error',
  code: 'UpdateFailedToLoad',
  updateId: '703a1adf',
  message: 'JS bundle threw',
};
const report = jest.fn();

beforeEach(() => {
  jest.clearAllMocks();
  Object.assign(mockUpdates, {
    isEnabled: true,
    isEmergencyLaunch: false,
    emergencyLaunchReason: null,
  });
});

describe('reportOtaLaunchFailures', () => {
  it('reports each update error once, then clears the log', async () => {
    mockUpdates.readLogEntriesAsync.mockResolvedValue([
      { ...crash, level: 'info' },
      crash,
      { ...crash, level: 'fatal', updateId: undefined },
    ]);
    await reportOtaLaunchFailures(report);
    expect(report.mock.calls).toEqual([
      ['UpdateFailedToLoad 703a1adf: JS bundle threw'],
      ['UpdateFailedToLoad -: JS bundle threw'],
    ]);
    expect(mockUpdates.clearLogEntriesAsync).toHaveBeenCalled();
  });

  it('reports an emergency launch with or without its reason', async () => {
    mockUpdates.readLogEntriesAsync.mockResolvedValue([]);
    mockUpdates.isEmergencyLaunch = true;
    mockUpdates.emergencyLaunchReason = 'update crashed on launch';
    await reportOtaLaunchFailures(report);
    mockUpdates.emergencyLaunchReason = null;
    await reportOtaLaunchFailures(report);
    expect(report.mock.calls).toEqual([
      ['emergency launch: update crashed on launch'],
      ['emergency launch: no reason'],
    ]);
    expect(mockUpdates.clearLogEntriesAsync).not.toHaveBeenCalled();
  });

  it('stays quiet on a clean launch and when updates are off', async () => {
    mockUpdates.readLogEntriesAsync.mockResolvedValue([{ ...crash, level: 'info' }]);
    await reportOtaLaunchFailures(report);
    mockUpdates.isEnabled = false;
    await reportOtaLaunchFailures(report);
    expect(report).not.toHaveBeenCalled();
    expect(mockUpdates.readLogEntriesAsync).toHaveBeenCalledTimes(1);
  });

  it('records a failure as a telemetry event by default', async () => {
    mockUpdates.readLogEntriesAsync.mockResolvedValue([crash]);
    await expect(reportOtaLaunchFailures()).resolves.toBeUndefined();
    expect(mockUpdates.clearLogEntriesAsync).toHaveBeenCalled();
  });
});
