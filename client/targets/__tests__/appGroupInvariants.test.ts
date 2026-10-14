import { readdirSync, readFileSync } from 'node:fs';
import * as path from 'node:path';

const APP_GROUP_ID = 'group.com.altune.app';
const APPLICATION_GROUPS_ENTITLEMENT = 'com.apple.security.application-groups';

const appJsonIos = require('@/../app.json').expo.ios;

const sharedDir = path.join(__dirname, '..', '_shared');
const appGroupSwift = readFileSync(path.join(sharedDir, 'AppGroup.swift'), 'utf8');

describe('app.json declares the App Group for sideload provisioning', () => {
  it('lists the group under ios.entitlements application-groups', () => {
    expect(appJsonIos.entitlements?.[APPLICATION_GROUPS_ENTITLEMENT]).toContain(APP_GROUP_ID);
  });

  it('lists the group under ios.infoPlist.ALTAppGroups so SideStore provisions it', () => {
    expect(appJsonIos.infoPlist?.ALTAppGroups).toContain(APP_GROUP_ID);
  });
});

describe('AppGroup.swift resolves the possibly-renamed group', () => {
  it('exposes the resolver named in the epic seam', () => {
    expect(appGroupSwift).toContain('func resolvedAppGroup() -> String');
  });

  it('reads the re-signed group from the embedded provisioning profile first', () => {
    expect(appGroupSwift).toContain('embedded');
    expect(appGroupSwift).toContain('mobileprovision');
    expect(appGroupSwift).toContain(APPLICATION_GROUPS_ENTITLEMENT);
  });

  it('falls back to ALTAppGroups in Info.plist', () => {
    expect(appGroupSwift).toContain('ALTAppGroups');
  });

  it('falls back to the hardcoded group id last', () => {
    expect(appGroupSwift).toContain(APP_GROUP_ID);
  });
});

describe('app.json App Group declaration, probed', () => {
  it('declares only the bare group id in both places, so SideStore can rename it once', () => {
    expect(appJsonIos.entitlements?.['com.apple.security.application-groups']).toEqual([
      'group.com.altune.app',
    ]);
    expect(appJsonIos.infoPlist?.ALTAppGroups).toEqual(['group.com.altune.app']);
  });

  it('keeps the existing Info.plist keys alongside ALTAppGroups', () => {
    expect(appJsonIos.infoPlist?.ITSAppUsesNonExemptEncryption).toBe(false);
    expect(appJsonIos.infoPlist?.UIBackgroundModes).toContain('audio');
  });
});

describe('targets/_shared is synced into every target', () => {
  it('holds only .swift files, so no test or fixture ships in the app', () => {
    const entries = readdirSync(sharedDir);
    expect(entries.length).toBeGreaterThan(0);
    for (const entry of entries) {
      expect(entry.endsWith('.swift')).toBe(true);
    }
  });
});
