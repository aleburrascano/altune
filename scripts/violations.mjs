import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const BASELINE = 'module-spec.baseline.json'
const root = resolve(process.argv[2] ?? join(dirname(fileURLToPath(import.meta.url)), '..'))

function baselines(dir) {
  const found = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory() && entry.name !== 'node_modules' && !entry.name.startsWith('.')) found.push(...baselines(join(dir, entry.name)))
    else if (entry.isFile() && entry.name === BASELINE) found.push(join(dir, entry.name))
  }
  return found
}

const out = []
for (const file of baselines(root).sort()) {
  const path = relative(root, file)
  const prefix = dirname(path) === '.' ? '' : `${dirname(path)}/`
  let saved
  try {
    saved = JSON.parse(readFileSync(file, 'utf8'))
  } catch (error) {
    console.error(`violations: ${path} is not JSON: ${error.message}`)
    process.exit(1)
  }
  for (const entry of saved.violations ?? []) {
    const m = /^(\S+) (\S+)/.exec(entry)
    if (!m) {
      console.error(`violations: ${path} has an entry that names no rule and file: ${entry}`)
      process.exit(1)
    }
    out.push(JSON.stringify({ rule: m[1], file: prefix + m[2], baseline: path, entry }))
  }
}
process.stdout.on('error', (error) => {
  if (error.code === 'EPIPE') process.exit(0)
  throw error
})
if (out.length > 0) process.stdout.write(out.join('\n') + '\n')
