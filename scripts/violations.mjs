import { readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(process.argv[2] ?? join(dirname(fileURLToPath(import.meta.url)), '..'))

const baselines = [
  { path: 'client/consistency.baseline', pattern: /^(MC-\S+) (src\/\S+) /, prefix: 'client/' },
  { path: 'client/module-shape.baseline', pattern: /^(\S+) (src\/\S+)( |$)/, prefix: 'client/' },
  { path: 'client/mock-ratchet.baseline', pattern: /^(\S+) (\S+)$/, prefix: 'client/', rule: 'MOCK', fileGroup: 1 },
  { path: 'services/go-api/module-shape.baseline', pattern: /^(G\d+) (internal\/[^:\s]+)/, prefix: 'services/go-api/' },
]

const out = []
for (const { path, pattern, prefix, rule, fileGroup } of baselines) {
  let text
  try {
    text = readFileSync(join(root, path), 'utf8')
  } catch {
    console.error(`violations: ${path} is missing`)
    process.exit(1)
  }
  const lines = text.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const entry = lines[i]
    if (entry === '') continue
    const m = pattern.exec(entry)
    if (!m) {
      console.error(`violations: ${path}:${i + 1} does not match`)
      process.exit(1)
    }
    const file = prefix + m[fileGroup ?? 2]
    out.push(JSON.stringify({ rule: rule ?? m[1], file, baseline: path, entry }))
  }
}
process.stdout.on('error', (error) => {
  if (error.code === 'EPIPE') process.exit(0)
  throw error
})
if (out.length > 0) process.stdout.write(out.join('\n') + '\n')
