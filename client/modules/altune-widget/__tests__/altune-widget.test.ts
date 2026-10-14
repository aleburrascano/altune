import type * as AltuneWidget from '@modules/altune-widget';

type WidgetCommandEmitter = {
  emit(eventName: 'onWidgetCommand', payload: { command: string }): void;
};

type FakeAppGroup = {
  snapshotJson: string | null;
  coversByFileName: Map<string, string>;
  widgetReloadCount: number;
};

type FakeNativeWidget = {
  appGroup: FakeAppGroup;
  commandsHeldByNative: string[];
  coverUrlsThatFailToDownload: Set<string>;
};

const NATIVE_MODULE_NAME = 'AltuneWidget';
const COVER_URL = 'https://covers.example.test/nocturne.jpg';

function installFakeNativeWidget(): FakeNativeWidget {
  const fake: FakeNativeWidget = {
    appGroup: { snapshotJson: null, coversByFileName: new Map(), widgetReloadCount: 0 },
    commandsHeldByNative: [],
    coverUrlsThatFailToDownload: new Set(),
  };
  globalThis.expo.modules[NATIVE_MODULE_NAME] = {
    writeSnapshot(json: string) {
      fake.appGroup.snapshotJson = json;
    },
    async writeCoverFromUrl(url: string, fileName: string) {
      if (fake.coverUrlsThatFailToDownload.has(url)) {
        throw new Error(`The widget cover download failed for ${url}`);
      }
      fake.appGroup.coversByFileName.set(fileName, url);
    },
    reloadWidget() {
      fake.appGroup.widgetReloadCount += 1;
    },
    drainPendingCommands(this: WidgetCommandEmitter) {
      for (const command of fake.commandsHeldByNative.splice(0)) {
        this.emit('onWidgetCommand', { command });
      }
    },
  };
  return fake;
}

function loadAltuneWidget(): typeof AltuneWidget {
  let altuneWidget: typeof AltuneWidget | undefined;
  jest.isolateModules(() => {
    altuneWidget = require('@modules/altune-widget');
  });
  if (!altuneWidget) throw new Error('altune-widget did not load');
  return altuneWidget;
}

afterEach(() => {
  delete globalThis.expo.modules[NATIVE_MODULE_NAME];
});

describe('altune-widget JS surface', () => {
  it('replays the commands native held, in order, to a listener once drained', () => {
    const fake = installFakeNativeWidget();
    const altuneWidget = loadAltuneWidget();
    fake.commandsHeldByNative.push('next', 'shuffle', 'play-pause');
    const received: string[] = [];

    altuneWidget.addWidgetCommandListener((event) => received.push(event.command));
    altuneWidget.drainPendingCommands();

    expect(received).toEqual(['next', 'shuffle', 'play-pause']);
  });

  it('stops delivering commands to a listener once its subscription is removed', () => {
    const fake = installFakeNativeWidget();
    const altuneWidget = loadAltuneWidget();
    const received: string[] = [];
    const subscription = altuneWidget.addWidgetCommandListener((event) =>
      received.push(event.command),
    );
    fake.commandsHeldByNative.push('repeat');

    subscription.remove();
    altuneWidget.drainPendingCommands();

    expect(received).toEqual([]);
  });

  it('writes the snapshot json into the App Group unchanged', () => {
    const fake = installFakeNativeWidget();
    const altuneWidget = loadAltuneWidget();
    const snapshotJson = JSON.stringify({ title: 'Nocturne', isPlaying: true });

    altuneWidget.writeSnapshot(snapshotJson);

    expect(fake.appGroup.snapshotJson).toBe(snapshotJson);
  });

  it('resolves once native has written the cover from its url under the file name it was given', async () => {
    const fake = installFakeNativeWidget();
    const altuneWidget = loadAltuneWidget();

    await altuneWidget.writeCoverFromUrl(COVER_URL, 'cover.jpg');

    expect(fake.appGroup.coversByFileName.get('cover.jpg')).toBe(COVER_URL);
  });

  it('rejects when native fails to write the cover', async () => {
    const fake = installFakeNativeWidget();
    const altuneWidget = loadAltuneWidget();
    fake.coverUrlsThatFailToDownload.add(COVER_URL);

    await expect(altuneWidget.writeCoverFromUrl(COVER_URL, 'cover.jpg')).rejects.toThrow(
      COVER_URL,
    );
  });

  it('asks WidgetKit to reload each time reloadWidget is called', () => {
    const fake = installFakeNativeWidget();
    const altuneWidget = loadAltuneWidget();

    altuneWidget.reloadWidget();
    altuneWidget.reloadWidget();

    expect(fake.appGroup.widgetReloadCount).toBe(2);
  });

});

describe('altune-widget where the native module is not compiled in (Android, web)', () => {
  it('loads, and every call does nothing without throwing', () => {
    const altuneWidget = loadAltuneWidget();

    expect(() => {
      altuneWidget.writeSnapshot('{}');
      altuneWidget.reloadWidget();
      altuneWidget.drainPendingCommands();
    }).not.toThrow();
  });

  it('resolves the cover write without writing anything', async () => {
    const altuneWidget = loadAltuneWidget();

    await expect(altuneWidget.writeCoverFromUrl(COVER_URL, 'cover.jpg')).resolves.toBeUndefined();
  });

  it('subscribes a listener that is never called and removes without throwing', () => {
    const altuneWidget = loadAltuneWidget();
    const listener = jest.fn();

    const subscription = altuneWidget.addWidgetCommandListener(listener);
    altuneWidget.drainPendingCommands();

    expect(() => subscription.remove()).not.toThrow();
    expect(listener).not.toHaveBeenCalled();
  });
});

describe('altune-widget command delivery (probe)', () => {
  it('delivers each held command once even when drained repeatedly, keeping order across drains', () => {
    const fake = installFakeNativeWidget();
    const altuneWidget = loadAltuneWidget();
    const received: string[] = [];
    altuneWidget.addWidgetCommandListener((event) => received.push(event.command));

    fake.commandsHeldByNative.push('next', 'play-pause');
    altuneWidget.drainPendingCommands();
    altuneWidget.drainPendingCommands();
    fake.commandsHeldByNative.push('previous');
    altuneWidget.drainPendingCommands();

    expect(received).toEqual(['next', 'play-pause', 'previous']);
  });

  it('hands every pinned widget command string to the listener exactly as native sent it', () => {
    const fake = installFakeNativeWidget();
    const altuneWidget = loadAltuneWidget();
    const received: string[] = [];
    altuneWidget.addWidgetCommandListener((event) => received.push(event.command));
    fake.commandsHeldByNative.push('play-pause', 'next', 'previous', 'shuffle', 'repeat');

    altuneWidget.drainPendingCommands();

    expect(received).toEqual(['play-pause', 'next', 'previous', 'shuffle', 'repeat']);
  });

  it('gives each listener every command once, and removing one listener leaves the other subscribed', () => {
    const fake = installFakeNativeWidget();
    const altuneWidget = loadAltuneWidget();
    const first: string[] = [];
    const second: string[] = [];
    const firstSubscription = altuneWidget.addWidgetCommandListener((event) =>
      first.push(event.command),
    );
    altuneWidget.addWidgetCommandListener((event) => second.push(event.command));

    fake.commandsHeldByNative.push('shuffle');
    altuneWidget.drainPendingCommands();
    firstSubscription.remove();
    fake.commandsHeldByNative.push('repeat');
    altuneWidget.drainPendingCommands();

    expect(first).toEqual(['shuffle']);
    expect(second).toEqual(['shuffle', 'repeat']);
  });
});
