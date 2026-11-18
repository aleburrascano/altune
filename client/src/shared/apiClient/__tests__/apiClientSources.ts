import * as fs from 'fs';
import * as path from 'path';

const API_CLIENT_DIR = path.join(__dirname, '..');

export function apiClientSourceFiles(): string[] {
  return fs
    .readdirSync(API_CLIENT_DIR, { recursive: true, encoding: 'utf8' })
    .filter((file) => file.endsWith('.ts') && !file.split(path.sep).includes('__tests__'))
    .sort()
    .map((file) => path.join(API_CLIENT_DIR, file));
}

export function apiClientPath(file: string): string {
  return path.relative(API_CLIENT_DIR, file);
}
