import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

import { QueryClient } from '@tanstack/react-query';

import { applyServerEvent } from '../applyServerEvent';
import {
  SERVER_EVENT_TYPES,
  _resetUnhandledEventsForTest,
  unhandledEventTypes,
} from '../eventTypes';

const REPO_ROOT = join(__dirname, '..', '..', '..', '..', '..', '..');
const GO_ROOT = join(REPO_ROOT, 'services', 'go-api');

const PUBLISH_LITERAL = /\.Publish\([^,]+,\s*"([a-z_]+)"/g;
const SSE_LITERAL = /event:\s*([a-z_]+)\\n/g;

function goSourceFiles(): string[] {
  const listed = execFileSync('git', ['ls-files', '*.go'], {
    cwd: GO_ROOT,
    encoding: 'utf8',
  });
  return listed
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0 && !line.endsWith('_test.go'))
    .map((rel) => join(GO_ROOT, rel));
}

function publishedEventTypes(): Set<string> {
  const found = new Set<string>();
  for (const file of goSourceFiles()) {
    const source = readFileSync(file, 'utf8');
    for (const match of source.matchAll(PUBLISH_LITERAL)) {
      if (match[1]) found.add(match[1]);
    }
    for (const match of source.matchAll(SSE_LITERAL)) {
      if (match[1]) found.add(match[1]);
    }
  }
  return found;
}

const goApiPresent = existsSync(GO_ROOT);
const describeContract = goApiPresent ? describe : describe.skip;

describeContract('server event contract', () => {
  it('handles every event type the backend publishes', () => {
    const published = [...publishedEventTypes()].sort();
    expect(published.length).toBeGreaterThan(0);

    const unhandled = published.filter(
      (type) => !(SERVER_EVENT_TYPES as readonly string[]).includes(type),
    );
    expect(unhandled).toEqual([]);
  });

  it('declares no event type the backend never publishes', () => {
    const published = publishedEventTypes();
    const orphaned = SERVER_EVENT_TYPES.filter((type) => !published.has(type));
    expect(orphaned).toEqual([]);
  });
});

describe('unknown event types', () => {
  beforeEach(() => {
    _resetUnhandledEventsForTest();
  });

  it('are recorded rather than silently dropped', () => {
    const qc = new QueryClient();
    applyServerEvent(qc, { id: '1', type: 'track_reacquired', data: {} });

    expect(unhandledEventTypes()).toEqual(['track_reacquired']);
  });

  it('leave a known event unaffected', () => {
    const qc = new QueryClient();
    const spy = jest.spyOn(qc, 'invalidateQueries');

    applyServerEvent(qc, { id: '1', type: 'not_a_real_event', data: {} });
    expect(spy).not.toHaveBeenCalled();

    applyServerEvent(qc, { id: '2', type: 'resync', data: {} });
    expect(spy).toHaveBeenCalled();
  });
});
