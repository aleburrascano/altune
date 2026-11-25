const SHORT_UPDATE_ID_LENGTH = 8;

const BUILD_COMMIT_LENGTH = 12;

export function versionOf(manifestVersion: string | undefined): string {
  return manifestVersion ?? 'dev';
}

export function versionLabel(version: string, runningUpdateId: string | null): string {
  if (runningUpdateId == null) return version;
  return `${version} · update ${runningUpdateId.slice(0, SHORT_UPDATE_ID_LENGTH)}`;
}

export function telemetryVersionOf(commit: string | undefined, label: string): string {
  return `${commit?.slice(0, BUILD_COMMIT_LENGTH) || 'dev'}/${label}`;
}
