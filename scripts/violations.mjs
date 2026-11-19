import { spawnSync } from 'node:child_process'
import { existsSync, statSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scripts = dirname(fileURLToPath(import.meta.url))
const kit = join(scripts, '..', 'node_modules', '.bin', 'module-spec')
const BUFFER = 64 * 1024 * 1024

function fail(message, code = 1) {
  console.error(`violations: ${message}`)
  process.exit(code)
}

function run(command, args, what, cwd) {
  const result = spawnSync(command, args, { cwd, encoding: 'utf8', maxBuffer: BUFFER })
  if (result.error) fail(`${what} could not run: ${result.error.message}`)
  if (result.status !== 0) fail(`${what} exited ${result.status}: ${result.stderr.trim()}`)
  return result.stdout
}

const given = resolve(process.argv[2] ?? join(scripts, '..'))
if (!existsSync(given) || !statSync(given).isDirectory()) fail(`${given} is not a folder`, 2)
const root = run('git', ['rev-parse', '--show-toplevel'], `git rev-parse in ${given}`, given).trim()
if (!existsSync(kit)) fail('the module-spec kit is not installed. Run pnpm install at the repo root (it fetches the kit from gitea).', 3)

const roots = run('bash', [join(scripts, 'module-spec.sh'), 'roots'], 'scripts/module-spec.sh roots', root).split('\n').filter(Boolean)
const out = []
for (const dir of roots) {
  const prefix = dir === '.' ? '' : `${dir}/`
  const baseline = `${prefix}module-spec.baseline.json`
  const keys = run(kit, ['baseline', '--list', '--root', dir], `module-spec baseline --list --root ${dir}`, root).split('\n').filter(Boolean)
  for (const entry of keys) {
    const m = /^(\S+) (\S+)/.exec(entry)
    if (!m) fail(`the ${dir} baseline has an entry that names no rule and file: ${entry}`)
    out.push(JSON.stringify({ rule: m[1], file: prefix + m[2], baseline, entry }))
  }
}
process.stdout.on('error', (error) => {
  if (error.code === 'EPIPE') process.exit(0)
  throw error
})
if (out.length > 0) process.stdout.write(out.join('\n') + '\n')
