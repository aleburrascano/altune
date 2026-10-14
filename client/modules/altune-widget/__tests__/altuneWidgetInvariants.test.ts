import { existsSync, readdirSync, readFileSync } from 'node:fs';
import * as path from 'node:path';

import { getMirrorStateObject } from 'expo-modules-autolinking/build/inlineModules/inlineModules';

const appRoot = path.join(__dirname, '..', '..', '..');
const moduleRoot = path.join(__dirname, '..');
const sharedSwiftDir = path.join(appRoot, 'targets', '_shared');
const moduleSwiftPath = path.join(moduleRoot, 'ios', 'AltuneWidgetModule.swift');
const intentBridgePath = path.join(sharedSwiftDir, 'IntentBridge.swift');

const FILES_THAT_MAKE_AUTOLINKING_BUILD_A_SEPARATE_POD = ['expo-module.config.json'];
const POD_SPEC_EXTENSION = '.podspec';

const appExperiments = require('@/../app.json').expo.experiments;

function readIfPresent(filePath: string): string {
  return existsSync(filePath) ? readFileSync(filePath, 'utf8') : '';
}

describe('the AltuneWidget inline module compiles into the app target', () => {
  it('is discovered by autolinking from the folders app.json watches', async () => {
    const mirror = await getMirrorStateObject({
      watchedDirectories: appExperiments.inlineModules?.watchedDirectories,
      appRoot,
    });

    expect(mirror.swiftModuleClassNames).toEqual(['AltuneWidgetModule']);
  });

  it('keeps typed routes on beside the inline modules experiment', () => {
    expect(appExperiments.typedRoutes).toBe(true);
  });

  it('holds a Swift inline module and no podspec or module config that would split it into its own pod', () => {
    const moduleFiles = existsSync(moduleRoot) ? readdirSync(moduleRoot) : [];
    const podFiles = moduleFiles.filter(
      (file) =>
        file.endsWith(POD_SPEC_EXTENSION) ||
        FILES_THAT_MAKE_AUTOLINKING_BUILD_A_SEPARATE_POD.includes(file),
    );

    expect(readIfPresent(moduleSwiftPath)).toContain('func definition() -> ModuleDefinition');
    expect(podFiles).toEqual([]);
  });

  it('is named AltuneWidget, emits onWidgetCommand, and takes over IntentBridge', () => {
    const moduleSwift = readIfPresent(moduleSwiftPath);

    expect(moduleSwift).toContain('Name("AltuneWidget")');
    expect(moduleSwift).toContain('"onWidgetCommand"');
    expect(moduleSwift).toContain('IntentBridge.handler =');
  });

  it('exposes every function the JS surface calls', () => {
    const moduleSwift = readIfPresent(moduleSwiftPath);

    for (const functionName of ['writeSnapshot', 'reloadWidget', 'drainPendingCommands']) {
      expect(moduleSwift).toContain(`Function("${functionName}")`);
    }
    expect(moduleSwift).toContain('AsyncFunction("writeCoverFromUrl")');
    expect(moduleSwift).not.toContain('Function("writeCover")');
  });

  it('downloads the cover with a timeout, downscales it to the widget size and writes a JPEG atomically', () => {
    const moduleSwift = readIfPresent(moduleSwiftPath);

    expect(moduleSwift).toContain('URLSession.shared.data(for: request)');
    expect(moduleSwift).toContain('request.timeoutInterval = coverDownloadTimeoutSeconds');
    expect(moduleSwift).toContain('coverLongestSidePoints: CGFloat = 300');
    expect(moduleSwift).toContain('UIGraphicsImageRenderer(');
    expect(moduleSwift).toContain('.jpegData(compressionQuality: coverJpegQuality)');
    expect(moduleSwift).toContain('coverJpeg.write(to: coverFile, options: .atomic)');
  });

  it('rejects a cover on an HTTP error, undecodable data, or a file name that leaves the container', () => {
    const moduleSwift = readIfPresent(moduleSwiftPath);

    expect(moduleSwift).toContain('throw CoverDownloadFailedException(httpResponse.statusCode)');
    expect(moduleSwift).toContain('throw CoverIsNotAnImageException(');
    expect(moduleSwift).toContain('throw CoverFileNameEscapesContainerException(fileName)');
  });

  it('writes into the App Group the shared resolver names', () => {
    const moduleSwift = readIfPresent(moduleSwiftPath);

    expect(moduleSwift).toContain('UserDefaults(suiteName: resolvedAppGroup())');
    expect(moduleSwift).toContain('forSecurityApplicationGroupIdentifier: resolvedAppGroup()');
    expect(moduleSwift).toContain('WidgetCenter.shared.reloadAllTimelines()');
  });
});

describe('IntentBridge is shared by the intents and the module', () => {
  it('is a main-actor enum with a settable handler and a send that buffers', () => {
    const intentBridge = readIfPresent(intentBridgePath);

    expect(intentBridge).toContain('@MainActor\nenum IntentBridge');
    expect(intentBridge).toContain('static var handler: ((String) -> Void)?');
    expect(intentBridge).toContain('static func send(_ command: String)');
    expect(intentBridge).toContain('bufferedCommands.append(command)');
  });

  it('hands what it buffered to a handler as soon as one is set', () => {
    const intentBridge = readIfPresent(intentBridgePath);

    expect(intentBridge).toContain('didSet { deliverBufferedCommands() }');
  });

  it('ships into both bundles from targets/_shared as Swift only', () => {
    const sharedEntries = readdirSync(sharedSwiftDir);

    expect(sharedEntries).toContain('IntentBridge.swift');
    expect(sharedEntries.filter((entry) => !entry.endsWith('.swift'))).toEqual([]);
  });
});
