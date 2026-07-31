import * as fs from 'fs';
import * as path from 'path';

import type { AcquisitionPhase } from '../stagePhase';
import {
  ACQUISITION_PHASES,
  STAGE_TO_PHASE,
  phaseLabel,
  stageLabel,
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

const STEP_NAME_LITERAL = /func \(\w+ \*\w+Step\) Name\(\) string\s*\{\s*return "([a-zA-Z_]+)"/g;

function deriveAcquisitionStepNames(goApiRoot: string): Set<string> {
  const dir = path.join(goApiRoot, 'internal', 'acquisition', 'service');
  const names = new Set<string>();
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (!entry.isFile()) continue;
    if (!entry.name.startsWith('step_') || !entry.name.endsWith('.go')) continue;
    if (entry.name.endsWith('_test.go')) continue;
    const text = fs.readFileSync(path.join(dir, entry.name), 'utf8');
    for (const match of text.matchAll(STEP_NAME_LITERAL)) names.add(match[1]!);
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

describe('stageLabel', () => {
  it('composes stageToPhase and phaseLabel for a recognized stage', () => {
    expect(stageLabel('download')).toBe('Downloading…');
  });

  it('composes stageToPhase and phaseLabel for a falsy stage', () => {
    expect(stageLabel(null)).toBe('Working…');
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
