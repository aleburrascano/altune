function assertSupportedNode(version) {
  const major = Number.parseInt(String(version).split('.')[0], 10);
  if (major < 22) {
    throw new Error(
      `Mobile tests need Node 22 (running ${version}) per .nvmrc. ` +
        'Run `bash scripts/worktree-deps.sh` and export the PATH line it prints, then retry.',
    );
  }
}

module.exports = { assertSupportedNode };
