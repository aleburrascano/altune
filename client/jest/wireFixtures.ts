import * as fs from 'fs';
import * as path from 'path';

function findWireRoot(): string {
  let dir = __dirname;
  for (let hop = 0; hop < 12; hop += 1) {
    const candidate = path.join(dir, 'contracts', 'wire');
    if (fs.existsSync(candidate)) return candidate;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  throw new Error('contracts/wire not found above ' + __dirname);
}

const WIRE_ROOT = findWireRoot();

export function wireFixture(name: string): unknown {
  return JSON.parse(fs.readFileSync(path.join(WIRE_ROOT, `${name}.json`), 'utf8'));
}

