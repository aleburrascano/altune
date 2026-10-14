import { existsSync, readFileSync } from 'node:fs';
import * as path from 'node:path';

const APP_GROUP_ID = 'group.com.altune.app';
const PLAYBACK_INTENTS = [
  'PlayPauseIntent()',
  'NextIntent()',
  'PreviousIntent()',
  'ShuffleIntent()',
  'RepeatIntent()',
];

const targetsDir = path.join(__dirname, '..');
const spikeDir = path.join(targetsDir, 'carplay-widget');
const widgetDir = path.join(targetsDir, 'widget');
const widgetConfigPath = path.join(widgetDir, 'expo-target.config.js');
const widgetSwiftPath = path.join(widgetDir, 'AltuneWidget.swift');
const widgetInfoPlistPath = path.join(widgetDir, 'Info.plist');

function readIfPresent(filePath: string): string {
  return existsSync(filePath) ? readFileSync(filePath, 'utf8') : '';
}

describe('the now-playing widget target replaces the spike', () => {
  it('removes the carplay-widget tap-counter target', () => {
    expect(existsSync(spikeDir)).toBe(false);
  });

  it('declares a widget target named AltuneWidget through the apple-targets config', () => {
    const widgetConfig = existsSync(widgetConfigPath) ? require(widgetConfigPath) : {};

    expect(widgetConfig).toMatchObject({ type: 'widget', name: 'AltuneWidget' });
  });

  it('opts into entitlements so apple-targets mirrors the app App Group into the widget', () => {
    const widgetConfig = existsSync(widgetConfigPath) ? require(widgetConfigPath) : {};

    expect(widgetConfig.entitlements).toEqual({});
  });

  it('offers the small and medium families and no other', () => {
    const widgetSwift = readIfPresent(widgetSwiftPath);

    expect(widgetSwift).toContain('.supportedFamilies([.systemSmall, .systemMedium])');
  });

  it('declares the widgetkit extension point and the App Group SideStore provisions', () => {
    const infoPlist = readIfPresent(widgetInfoPlistPath);

    expect(infoPlist).toContain('<string>com.apple.widgetkit-extension</string>');
    expect(infoPlist).toMatch(
      new RegExp(
        `<key>ALTAppGroups</key>\\s*<array>\\s*<string>${APP_GROUP_ID}</string>\\s*</array>`,
      ),
    );
  });
});

describe('the widget reads the shared snapshot and drives playback', () => {
  it('reads the widgetSnapshot JSON from the resolved App Group suite', () => {
    const widgetSwift = readIfPresent(widgetSwiftPath);

    expect(widgetSwift).toContain('UserDefaults(suiteName: resolvedAppGroup())');
    expect(widgetSwift).toContain('"widgetSnapshot"');
  });

  it('loads the cover from the resolved App Group container', () => {
    const widgetSwift = readIfPresent(widgetSwiftPath);

    expect(widgetSwift).toMatch(
      /containerURL\(\s*forSecurityApplicationGroupIdentifier: resolvedAppGroup\(\)\s*\)/,
    );
    expect(widgetSwift).toContain('UIImage(contentsOfFile:');
  });

  it('decodes every pinned snapshot field', () => {
    const widgetSwift = readIfPresent(widgetSwiftPath);

    for (const field of [
      'signedIn: Bool',
      'nowPlaying: NowPlaying?',
      'title: String',
      'artist: String',
      'source: String?',
      'isPlaying: Bool',
      'shuffle: Bool',
      '`repeat`: RepeatMode',
      'positionSec: Double',
      'durationSec: Double',
      'coverFileName: String?',
    ]) {
      expect(widgetSwift).toContain(`let ${field}`);
    }
  });

  it('binds every control to its AudioPlaybackIntent', () => {
    const widgetSwift = readIfPresent(widgetSwiftPath);

    for (const intent of PLAYBACK_INTENTS) {
      expect(widgetSwift).toContain(`intent: ${intent}`);
    }
  });

  it('shows the nothing-played and signed-out empty states', () => {
    const widgetSwift = readIfPresent(widgetSwiftPath);

    expect(widgetSwift).toContain('"Open Altune to start a playlist"');
    expect(widgetSwift).toContain('"Sign in to Altune"');
  });
});
