export const DIFF_DETECT_FLAGS = ['-M', '-C', '--find-copies-harder'];
const FILE_HEADER = /^diff --git a\/.* b\/(.*)$/gm;
const HUNK = /^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@/;

function removedText(patch) {
  const removed = new Set();
  for (const line of patch.split('\n')) {
    if (!line.startsWith('-') || line.startsWith('---')) continue;
    const text = line.slice(1).trim();
    if (text) removed.add(text);
  }
  return removed;
}

function addedLinesOf(block, moved) {
  const added = new Set();
  let next = null;
  for (const line of block.split('\n')) {
    const hunk = HUNK.exec(line);
    if (hunk) {
      next = Number(hunk[1]);
      continue;
    }
    if (next === null || !line.startsWith('+')) continue;
    if (!moved.has(line.slice(1).trim())) added.add(next);
    next += 1;
  }
  return added;
}

function addedLinesPerFile(patch, moved, prefix, keep) {
  const headers = [...patch.matchAll(FILE_HEADER)];
  const byFile = new Map();
  headers.forEach((header, i) => {
    const file = header[1];
    if (!keep(file)) return;
    const block = patch.slice(header.index, headers[i + 1]?.index ?? patch.length);
    byFile.set(file.slice(prefix.length), addedLinesOf(block, moved));
  });
  return byFile;
}

export function addedLinesSince(git, base, prefix, keep) {
  const changes = ['--text', ...DIFF_DETECT_FLAGS, '-U0', base, '--', ':/'];
  const patch = git(['diff', '--diff-filter=ACMR', ...changes]);
  const moved = removedText(git(['diff', ...changes]));
  return addedLinesPerFile(patch, moved, prefix, keep);
}
