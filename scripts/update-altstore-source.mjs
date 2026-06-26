// Update apps.json (the AltStore / SideStore source) with a freshly released
// build. Run by .github/workflows/release-ios.yml after the IPA is published.
//
// Env: VERSION (e.g. 1.0.0), SIZE (ipa bytes), REPO (owner/repo).
// Effect: prepends/replaces the version in apps[0].versions and mirrors the
// latest into the legacy top-level fields for older clients.

import { readFileSync, writeFileSync } from "node:fs";

const VERSION = process.env.VERSION;
const SIZE = Number(process.env.SIZE);
const REPO = process.env.REPO; // owner/repo

if (!VERSION || !Number.isFinite(SIZE) || !REPO) {
  console.error("Missing VERSION / SIZE / REPO env");
  process.exit(1);
}

const SOURCE = "apps.json";
const MIN_OS = "15.1";
const date = new Date().toISOString().slice(0, 10);
const downloadURL = `https://github.com/${REPO}/releases/download/v${VERSION}/Altune.ipa`;

const source = JSON.parse(readFileSync(SOURCE, "utf8"));
const app = source.apps?.[0];
if (!app) {
  console.error("apps.json has no apps[0]");
  process.exit(1);
}

const entry = {
  version: VERSION,
  date,
  localizedDescription: `Altune v${VERSION}`,
  downloadURL,
  size: SIZE,
  minOSVersion: MIN_OS,
};

app.versions = Array.isArray(app.versions) ? app.versions : [];
app.versions = app.versions.filter((v) => v.version !== VERSION);
app.versions.unshift(entry);

// Legacy top-level mirror (older AltStore clients read these).
app.version = VERSION;
app.versionDate = date;
app.versionDescription = entry.localizedDescription;
app.downloadURL = downloadURL;
app.size = SIZE;
app.minOSVersion = MIN_OS;

writeFileSync(SOURCE, JSON.stringify(source, null, 2) + "\n");
console.log(`apps.json updated → v${VERSION} (${SIZE} bytes)`);
