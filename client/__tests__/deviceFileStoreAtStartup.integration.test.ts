jest.unmock('@shared/files');

const THEME_URI = 'file:///document/preferences/theme.json';

type FsDouble = {
  seedFile(uri: string, contents: string): void;
  readFile(uri: string): string | undefined;
};

function freshApp<T>(start: (fs: FsDouble) => T): T {
  let result!: T;
  jest.isolateModules(() => {
    result = start(require('expo-file-system').__fs);
  });
  return result;
}

const storedThroughTheDefault = freshApp((fs) => {
  require('@/mobile/installDeviceFileStore');
  const { defaultFileStore } = require('@shared/files');
  const dir = defaultFileStore.openDirectory('preferences');
  dir.create();
  dir.openFile('theme.json').write('"light"');
  return fs.readFile(THEME_URI);
});

const schemeAtLaunch = freshApp((fs) => {
  fs.seedFile(THEME_URI, JSON.stringify('light'));
  require('@/app/_layout');
  return require('@shared/ui').useThemePreference.getState().scheme;
});

describe('the device file store the app sets at startup', () => {
  it('is the device filesystem natively', () => {
    expect(storedThroughTheDefault).toBe('"light"');
  });

  it('is set before any module the root layout imports reads what it stored', () => {
    expect(schemeAtLaunch).toBe('light');
  });
});
