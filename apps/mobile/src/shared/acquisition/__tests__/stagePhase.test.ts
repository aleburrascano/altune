import * as fs from 'fs';
import * as path from 'path';

import type { AcquisitionPhase } from '../stagePhase';
import {
  ACQUISITION_PHASES,
  STAGE_TO_PHASE,
  phaseLabel,
  stageToPhase,
} from '../stagePhase';

function findGoApiRoot(): string | null {
  let dir = __dirname;
  for (let hop = 0; hop < 12; hop += 1) {
    const candidate = path.join(dir, 'services', 'go-api');
    if (fs.existsSync(candidate)) return candidate;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

type GoSource = { fileName: string; text: string };

// A step's Name() may return either a literal or a package-level constant —
// pipeline.go's stepName* block since #2094 — so the contract is only derivable
// by resolving the returned identifier the way the compiler does.
const STEP_NAME_RETURN = /func \(\w+ \*\w+Step\) Name\(\) string\s*\{\s*return\s+("[^"]*"|\w+)/g;
const CONST_BLOCK_BODY = /^const \(\n([\s\S]*?)\n\)/gm;
const CONST_SINGLE_LINE = /^const (.+)$/gm;
const STRING_CONST = /^\s*(\w+)(?:\s+\w+)?\s*=\s*"([^"]*)"/gm;

function readPackageSources(dir: string): GoSource[] {
  return fs
    .readdirSync(dir, { withFileTypes: true })
    .filter((entry) => entry.isFile() && entry.name.endsWith('.go'))
    .filter((entry) => !entry.name.endsWith('_test.go'))
    .map((entry) => ({
      fileName: entry.name,
      text: fs.readFileSync(path.join(dir, entry.name), 'utf8'),
    }));
}

function* constDeclarations(goSource: string): Generator<string> {
  for (const [, body] of goSource.matchAll(CONST_BLOCK_BODY)) yield body!;
  for (const [, line] of goSource.matchAll(CONST_SINGLE_LINE)) yield line!;
}

function goStringConstants(sources: GoSource[]): Map<string, string> {
  const constants = new Map<string, string>();
  for (const { text } of sources) {
    for (const declaration of constDeclarations(text)) {
      for (const [, name, value] of declaration.matchAll(STRING_CONST)) constants.set(name!, value!);
    }
  }
  return constants;
}

function resolveStepName(returned: string, constants: Map<string, string>): string {
  if (returned.startsWith('"')) return returned.slice(1, -1);
  const value = constants.get(returned);
  if (value === undefined) {
    throw new Error(
      `Name() returns ${returned}, which is no string constant of internal/acquisition/service`,
    );
  }
  return value;
}

function deriveAcquisitionStepNames(goApiRoot: string): Set<string> {
  const sources = readPackageSources(path.join(goApiRoot, 'internal', 'acquisition', 'service'));
  const constants = goStringConstants(sources);
  const names = new Set<string>();
  for (const { fileName, text } of sources) {
    if (!fileName.startsWith('step_')) continue;
    for (const [, returned] of text.matchAll(STEP_NAME_RETURN)) {
      names.add(resolveStepName(returned!, constants));
    }
  }
  return names;
}

const GO_API_ROOT = findGoApiRoot();

describe('stageToPhase', () => {
  it('falls back to working for a falsy stage', () => {
    expect(stageToPhase(null)).toBe('working');
    expect(stageToPhase(undefined)).toBe('working');
    expect(stageToPhase('')).toBe('working');
  });

  it('falls back to working for an unrecognized stage', () => {
    expect(stageToPhase('probe_new_step')).toBe('working');
  });

  it.each([
    ['search', 'finding'],
    ['select', 'finding'],
    ['download', 'downloading'],
    ['tag', 'finishing'],
    ['store', 'finishing'],
    ['update_track', 'finishing'],
  ] as const)('maps stage %s to phase %s', (stage, phase) => {
    expect(stageToPhase(stage)).toBe(phase);
  });
});

describe('ACQUISITION_PHASES', () => {
  it('lists exactly the three in-progress phases, in progress-bar order', () => {
    expect(ACQUISITION_PHASES).toEqual(['finding', 'downloading', 'finishing']);
  });
});

describe('phaseLabel', () => {
  it.each([
    ['finding', 'Finding source…'],
    ['downloading', 'Downloading…'],
    ['finishing', 'Finishing up…'],
    ['done', 'Done'],
    ['failed', 'Failed'],
    ['working', 'Working…'],
  ] as [AcquisitionPhase, string][])('labels phase %s as %j', (phase, label) => {
    expect(phaseLabel(phase)).toBe(label);
  });
});

describe('resolving what a Go step Name() returns', () => {
  it('reads through a constant declared in a const block or on its own line', () => {
    const constants = goStringConstants([
      { fileName: 'pipeline.go', text: 'const (\n\tstepNameTag = "tag"\n)\n' },
      { fileName: 'scheduler.go', text: 'const stepNameQueue = "queue_wait"\n' },
    ]);

    expect(resolveStepName('stepNameTag', constants)).toBe('tag');
    expect(resolveStepName('stepNameQueue', constants)).toBe('queue_wait');
  });

  it('reads a literal return unchanged', () => {
    expect(resolveStepName('"update_track"', new Map())).toBe('update_track');
  });

  it('names the identifier it could not resolve rather than deriving nothing', () => {
    expect(() => resolveStepName('stepNameGhost', new Map())).toThrow('stepNameGhost');
  });
});

describe('acquisition stage names, derived from services/go-api at test time', () => {
  it('finds the Go source tree to derive the contract from', () => {
    expect(GO_API_ROOT).not.toBeNull();
  });

  it('matches STAGE_TO_PHASE exactly, with no step unmapped and no mapping without a step', () => {
    const stepNames = deriveAcquisitionStepNames(GO_API_ROOT!);

    expect(stepNames.size).toBeGreaterThan(0);
    expect(Object.keys(STAGE_TO_PHASE).sort()).toEqual([...stepNames].sort());
  });
});
