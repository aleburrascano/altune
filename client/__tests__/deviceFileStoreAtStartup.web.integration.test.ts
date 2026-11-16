jest.unmock('@shared/files');

afterEach(() => {
  localStorage.clear();
});

describe('the device file store the app sets at startup on web', () => {
  it('keeps every file in localStorage', () => {
    require('@/mobile/installDeviceFileStore');
    const { defaultFileStore } = require('@shared/files');

    const dir = defaultFileStore.openDirectory('preferences');
    dir.create();
    dir.openFile('theme.json').write('"light"');

    expect(localStorage.getItem('altune:file:preferences/theme.json')).toBe('"light"');
  });
});
