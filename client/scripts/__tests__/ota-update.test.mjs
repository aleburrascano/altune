import assert from 'node:assert/strict';
import { createHash, generateKeyPairSync, verify } from 'node:crypto';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

import { stageUpdate } from '../ota-update.mjs';

const clientDir = dirname(dirname(dirname(fileURLToPath(import.meta.url))));
const BASE_URL = 'https://altune.example';

function b64urlSha256(bytes) {
  return createHash('sha256').update(bytes).digest('base64url');
}

function fakeExport() {
  const dir = mkdtempSync(join(tmpdir(), 'ota-export-'));
  mkdirSync(join(dir, '_expo/static/js/ios'), { recursive: true });
  mkdirSync(join(dir, 'assets'));
  const bundle = Buffer.from('hermes bytecode');
  const icon = Buffer.from('png bytes');
  const iconMd5 = createHash('md5').update(icon).digest('hex');
  writeFileSync(join(dir, '_expo/static/js/ios/entry-1.hbc'), bundle);
  writeFileSync(join(dir, 'assets', iconMd5), icon);
  writeFileSync(
    join(dir, 'metadata.json'),
    JSON.stringify({
      version: 0,
      bundler: 'metro',
      fileMetadata: {
        ios: { bundle: '_expo/static/js/ios/entry-1.hbc', assets: [{ path: `assets/${iconMd5}`, ext: 'png' }] },
      },
    }),
  );
  return { dir, bundle, icon, iconMd5 };
}

function keyPair() {
  return generateKeyPairSync('rsa', {
    modulusLength: 2048,
    publicKeyEncoding: { type: 'spki', format: 'pem' },
    privateKeyEncoding: { type: 'pkcs8', format: 'pem' },
  });
}

function stage(exp, privateKey) {
  const outDir = mkdtempSync(join(tmpdir(), 'ota-out-'));
  stageUpdate({
    exportDir: exp.dir,
    outDir,
    platform: 'ios',
    runtimeVersion: 'abc123',
    baseUrl: BASE_URL,
    privateKeyPem: privateKey,
    expoConfig: { name: 'Altune' },
  });
  return outDir;
}

function readPart(outDir) {
  const body = readFileSync(join(outDir, 'manifests/abc123/ios'), 'utf8');
  const match = body.match(/^--altune-ota\r\n([\s\S]*?)\r\n\r\n([\s\S]*)\r\n--altune-ota--\r\n$/);
  assert.ok(match, `not a single-part multipart body: ${body.slice(0, 200)}`);
  const headers = Object.fromEntries(
    match[1].split('\r\n').map((line) => {
      const at = line.indexOf(':');
      return [line.slice(0, at).toLowerCase(), line.slice(at + 1).trim()];
    }),
  );
  return { headers, manifestText: match[2] };
}

test('the manifest points every exported file at our server by its sha256', () => {
  const exp = fakeExport();
  const outDir = stage(exp, keyPair().privateKey);
  const { headers, manifestText } = readPart(outDir);
  const manifest = JSON.parse(manifestText);

  assert.match(headers['content-disposition'], /name="manifest"/);
  assert.equal(manifest.runtimeVersion, 'abc123');
  assert.match(manifest.id, /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
  assert.deepEqual(manifest.extra, { expoClient: { name: 'Altune' } });

  const bundleHash = b64urlSha256(exp.bundle);
  assert.equal(manifest.launchAsset.hash, bundleHash);
  assert.equal(manifest.launchAsset.contentType, 'application/javascript');
  assert.equal(manifest.launchAsset.url, `${BASE_URL}/updates/assets/${bundleHash}.js`);
  assert.deepEqual(readFileSync(join(outDir, `assets/${bundleHash}.js`)), exp.bundle);

  const iconHash = b64urlSha256(exp.icon);
  assert.deepEqual(manifest.assets, [
    {
      hash: iconHash,
      key: exp.iconMd5,
      fileExtension: '.png',
      contentType: 'image/png',
      url: `${BASE_URL}/updates/assets/${iconHash}.png`,
    },
  ]);
  assert.deepEqual(readFileSync(join(outDir, `assets/${iconHash}.png`)), exp.icon);
  rmSync(exp.dir, { recursive: true });
  rmSync(outDir, { recursive: true });
});

test('the manifest is signed by the publish key and no other key verifies it', () => {
  const exp = fakeExport();
  const publishKey = keyPair();
  const outDir = stage(exp, publishKey.privateKey);
  const { headers, manifestText } = readPart(outDir);

  const signature = headers['expo-signature'].match(/^sig="([^"]+)", keyid="main"$/);
  assert.ok(signature, `expo-signature was '${headers['expo-signature']}'`);
  const sig = Buffer.from(signature[1], 'base64');
  assert.equal(verify('sha256', Buffer.from(manifestText), publishKey.publicKey, sig), true);
  assert.equal(verify('sha256', Buffer.from(manifestText), keyPair().publicKey, sig), false);
  rmSync(exp.dir, { recursive: true });
  rmSync(outDir, { recursive: true });
});

test('the same export publishes under the same update id', () => {
  const exp = fakeExport();
  const key = keyPair().privateKey;
  const first = stage(exp, key);
  const second = stage(exp, key);
  assert.equal(JSON.parse(readPart(first).manifestText).id, JSON.parse(readPart(second).manifestText).id);
  rmSync(exp.dir, { recursive: true });
  rmSync(first, { recursive: true });
  rmSync(second, { recursive: true });
});

test('an exported asset type with no known content type stops the publish', () => {
  const exp = fakeExport();
  const metadata = JSON.parse(readFileSync(join(exp.dir, 'metadata.json'), 'utf8'));
  metadata.fileMetadata.ios.assets[0].ext = 'xyz';
  writeFileSync(join(exp.dir, 'metadata.json'), JSON.stringify(metadata));
  assert.throws(() => stage(exp, keyPair().privateKey), /xyz/);
  rmSync(exp.dir, { recursive: true });
});

test('the app takes updates from our server only when signed by our key', () => {
  const { expo } = JSON.parse(readFileSync(join(clientDir, 'app.json'), 'utf8'));
  assert.equal(expo.updates.url, 'https://altune.duckdns.org/updates/manifest');
  assert.deepEqual(expo.updates.codeSigningMetadata, { keyid: 'main', alg: 'rsa-v1_5-sha256' });
  assert.ok(existsSync(join(clientDir, expo.updates.codeSigningCertificate)), 'the signing certificate is missing');
  assert.deepEqual(expo.runtimeVersion, { policy: 'fingerprint' });
});
