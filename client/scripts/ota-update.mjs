import { createHash, sign } from 'node:crypto';
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const BOUNDARY = 'altune-ota';
const CONTENT_TYPES = {
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  gif: 'image/gif',
  webp: 'image/webp',
  ttf: 'font/ttf',
  otf: 'font/otf',
};

function contentTypeOf(ext) {
  const type = CONTENT_TYPES[ext];
  if (!type) throw new Error(`no content type for exported .${ext} assets; add it to CONTENT_TYPES`);
  return type;
}

function asUuid(hex) {
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20, 32)}`;
}

function publishFile(exportDir, outDir, baseUrl, relPath, ext) {
  const bytes = readFileSync(join(exportDir, relPath));
  const hash = createHash('sha256').update(bytes).digest('base64url');
  const name = `${hash}.${ext}`;
  copyFileSync(join(exportDir, relPath), join(outDir, 'assets', name));
  const key = createHash('md5').update(bytes).digest('hex');
  return { hash, key, url: `${baseUrl}/updates/assets/${name}` };
}

function buildManifest({ exportDir, outDir, platform, runtimeVersion, baseUrl, expoConfig }) {
  const metadataBytes = readFileSync(join(exportDir, 'metadata.json'));
  const files = JSON.parse(metadataBytes.toString('utf8')).fileMetadata[platform];
  const launch = publishFile(exportDir, outDir, baseUrl, files.bundle, 'js');
  const assets = files.assets.map(({ path, ext }) => {
    const contentType = contentTypeOf(ext);
    const file = publishFile(exportDir, outDir, baseUrl, path, ext);
    return { hash: file.hash, key: file.key, fileExtension: `.${ext}`, contentType, url: file.url };
  });
  const id = createHash('sha256').update(runtimeVersion).update(metadataBytes).digest('hex');
  return {
    id: asUuid(id),
    createdAt: new Date().toISOString(),
    runtimeVersion,
    launchAsset: { hash: launch.hash, key: launch.key, contentType: 'application/javascript', url: launch.url },
    assets,
    metadata: {},
    extra: { expoClient: expoConfig },
  };
}

function signedMultipart(manifestText, privateKeyPem) {
  const sig = sign('sha256', Buffer.from(manifestText), privateKeyPem).toString('base64');
  return [
    `--${BOUNDARY}`,
    'content-type: application/json; charset=utf-8',
    'content-disposition: form-data; name="manifest"',
    `expo-signature: sig="${sig}", keyid="main"`,
    '',
    manifestText,
    `--${BOUNDARY}--`,
    '',
  ].join('\r\n');
}

export function stageUpdate(options) {
  const { outDir, platform, runtimeVersion, privateKeyPem } = options;
  mkdirSync(join(outDir, 'assets'), { recursive: true });
  const manifestText = JSON.stringify(buildManifest(options));
  mkdirSync(join(outDir, 'manifests', runtimeVersion), { recursive: true });
  writeFileSync(join(outDir, 'manifests', runtimeVersion, platform), signedMultipart(manifestText, privateKeyPem));
}

function requiredEnv(name) {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is not set`);
  return value;
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  const [exportDir, outDir, expoConfigFile] = process.argv.slice(2);
  stageUpdate({
    exportDir,
    outDir,
    platform: process.env.OTA_PLATFORM ?? 'ios',
    runtimeVersion: requiredEnv('OTA_RUNTIME_VERSION'),
    baseUrl: requiredEnv('OTA_BASE_URL'),
    privateKeyPem: readFileSync(requiredEnv('OTA_KEY_FILE'), 'utf8'),
    expoConfig: JSON.parse(readFileSync(expoConfigFile, 'utf8')),
  });
}
