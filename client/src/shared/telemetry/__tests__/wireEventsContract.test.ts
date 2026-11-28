import * as fs from 'fs';
import * as path from 'path';

import { getSessionId, recordEvent } from '@shared/telemetry';
import type { ClientEventType } from '@shared/telemetry';
import { signInTestUser } from '@testing/signInTestUser';
import { wireFixture } from '@testing/wireFixtures';
import { EVENT_ROUTES } from '@testing/eventRoutes';

const { __http } = require('@testing/doubles/fetch.js');

const CLIENT_EVENT_TYPES: Record<ClientEventType, true> = {
  results_shown: true,
  result_clicked: true,
  play: true,
  skip: true,
  completed: true,
  library_add: true,
  wrong_album: true,
  search_failed: true,
  search_degraded: true,
  playback_health: true,
  detail_health: true,
  acquisition_ui: true,
  client_error: true,
  user_action: true,
  failure_shown: true,
  sse_reconnect: true,
  outbox_flush_failed: true,
  download_failed: true,
  screen_view: true,
  play_start: true,
};

const clientTypes = Object.keys(CLIENT_EVENT_TYPES).sort();
const knownTypes = wireFixture('discovery_event_types') as string[];
const submittableTypes = wireFixture('discovery_event_types_client_submittable') as string[];
const payloadTypes = wireFixture('discovery_event_payload_types') as Record<string, string[]>;

const CLIENT_SRC = path.join(__dirname, '..', '..', '..');

const TYPESCRIPT_FILE = /\.tsx?$/;
const SKIPPED_DIRECTORIES = new Set(['__tests__', 'node_modules']);

function clientSourceFiles(dir: string): string[] {
  const files: string[] = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!SKIPPED_DIRECTORIES.has(entry.name)) files.push(...clientSourceFiles(full));
    } else if (TYPESCRIPT_FILE.test(entry.name) && !entry.name.endsWith('.d.ts')) {
      files.push(full);
    }
  }
  return files;
}

beforeEach(async () => {
  await signInTestUser();
});

describe('event types: the server fixtures against the ClientEventType the client declares', () => {
  it('every type the client declares is a name the server knows', () => {
    expect(clientTypes.filter((name) => !knownTypes.includes(name))).toEqual([]);
  });

  it('every type the client declares is one the server accepts at POST /v1/discovery/events', () => {
    expect(clientTypes.filter((name) => !submittableTypes.includes(name))).toEqual([]);
  });

  it('every type the server accepts from a client is one the client declares', () => {
    expect(submittableTypes.filter((name) => !clientTypes.includes(name))).toEqual([]);
  });

  it('results_shown is client-submittable and search_performed is server-only', () => {
    expect(submittableTypes).toContain('results_shown');
    expect(knownTypes).toContain('search_performed');
    expect(submittableTypes).not.toContain('search_performed');
  });
});

describe('request envelope: the keys the client sends against req_discovery_events', () => {
  it('sends only keys the server request fixture has, and every envelope key it has', async () => {
    __http.reply(EVENT_ROUTES, { status: 204 });
    const fixtureKeys = Object.keys(wireFixture('req_discovery_events') as object).sort();

    await recordEvent({
      type: 'play',
      search_id: 'search-1',
      event_id: 'evt-1',
      client_occurred_at: '2026-07-31T00:00:00.000Z',
    });

    expect(Object.keys(JSON.parse(__http.last().body)).sort()).toEqual(fixtureKeys);
  });
});

describe('payload value types: what the server fixture says each key accepts', () => {
  it('accepts a string for session_id and refuses a number, a boolean and null', () => {
    expect(payloadTypes['session_id']).toEqual(['string']);
  });

  it('the session_id recordEvent stamps is of a type the server accepts', async () => {
    __http.reply(EVENT_ROUTES, { status: 204 });

    await recordEvent({ type: 'play' });

    const stamped = JSON.parse(__http.last().body).payload.session_id;
    expect(payloadTypes['session_id']).toContain(typeof stamped);
    expect(stamped).toBe(getSessionId());
  });

  it('no client source assigns null to a payload key the server refuses as null', () => {
    const refusingNull = Object.entries(payloadTypes)
      .filter(([, accepted]) => !accepted.includes('null'))
      .map(([key]) => key);
    const offenders: string[] = [];

    for (const file of clientSourceFiles(CLIENT_SRC)) {
      const source = fs.readFileSync(file, 'utf8');
      for (const key of refusingNull) {
        if (new RegExp(`\\b${key}\\s*:\\s*[^,;\\n}]*\\bnull\\b`).test(source)) {
          offenders.push(`${path.relative(CLIENT_SRC, file)}:${key}`);
        }
      }
    }

    expect(refusingNull.length).toBeGreaterThan(0);
    expect(offenders).toEqual([]);
  });
});
