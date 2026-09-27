import { describe, expect, onTestFinished, test } from "vitest";
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import process from "node:process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const script = join(dirname(fileURLToPath(import.meta.url)), "..", "lint-changed-comments.mjs");

function git(cwd, ...args) {
  const r = spawnSync("git", args, { cwd, encoding: "utf8" });
  expect(r.status, r.stderr).toBe(0);
  return r.stdout.trim();
}

function write(root, files) {
  for (const [path, body] of Object.entries(files)) {
    mkdirSync(dirname(join(root, path)), { recursive: true });
    writeFileSync(join(root, path), body);
  }
}

function repo(baseFiles, changedFiles) {
  const root = mkdtempSync(join(tmpdir(), "lint-changed-comments-"));
  onTestFinished(() => rmSync(root, { recursive: true, force: true }));
  git(root, "init", "-q");
  git(root, "config", "user.email", "probe@example.com");
  git(root, "config", "user.name", "probe");
  git(root, "config", "commit.gpgsign", "false");
  write(root, { "src/keep.ts": "export const keep = 0;\n", ...baseFiles });
  git(root, "add", "-A");
  git(root, "commit", "-qm", "base");
  const base = git(root, "rev-parse", "HEAD");
  write(root, changedFiles);
  git(root, "add", "-A");
  git(root, "commit", "-qm", "change", "--allow-empty");
  return { root, base };
}

function lint(cwd, ...args) {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [script, ...args], { cwd });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (d) => (stdout += d));
    child.stderr.on("data", (d) => (stderr += d));
    child.on("close", (status) => resolve({ status, stdout, stderr }));
  });
}

describe.concurrent("lint-changed-comments", { timeout: 60000 }, () => {
  test("flags a comment added to source, tests, TSX and scripts, naming file and line", async () => {
    const { root, base } = repo(
      { "src/api.ts": "export const a = 1;\n", "src/api.test.ts": "export const b = 1;\n" },
      {
        "src/api.ts": "export const a = 1;\n// note\n",
        "src/api.test.ts": "export const b = 1;\n// note\n",
        "src/charts/Chart.tsx": "export const c = <div>{/* inner */}</div>;\n",
        "scripts/tool.mjs": "export default 1; /* note */\n",
        "scripts/tool.cjs": "module.exports = 1;\n\n// note\n",
      },
    );
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(1);
    for (const hit of [
      "src/api.ts:2 ",
      "src/api.test.ts:2 ",
      "src/charts/Chart.tsx:1 ",
      "scripts/tool.mjs:1 ",
      "scripts/tool.cjs:3 ",
    ]) {
      expect(r.stdout).toContain(hit);
    }
  });

  test("flags a new suppression on an added line", async () => {
    const { root, base } = repo({}, { "src/x.ts": "// @ts-ignore\nexport const k = 1;\n" });
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(1);
    expect(r.stdout).toMatch(/src\/x\.ts:1\s+new suppression/);
  });

  test("passes strings, template literals, regexes and JSX text that contain slashes or URLs", async () => {
    const { root, base } = repo(
      {},
      {
        "src/api.ts": [
          "export const url = 'https://x.dev';",
          'export const s = "a // b /* c */";',
          "export const t = `c // d`;",
          "export const r = /\\/\\//;",
          "",
        ].join("\n"),
        "src/Link.tsx": 'export const j = <a href="https://x.dev">see http://y.dev</a>;\n',
      },
    );
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(0);
  });

  test("passes a change that only deletes comment lines", async () => {
    const { root, base } = repo(
      { "src/old.ts": "// old\nexport const a = 1;\n// older\nexport const b = 2;\n" },
      { "src/old.ts": "export const a = 1;\nexport const b = 2;\n" },
    );
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(0);
  });

  test("passes a pure rename of a file whose comments already existed on base", async () => {
    const { root, base } = repo({ "src/before.ts": "// kept\nexport const a = 1;\n" }, {});
    git(root, "mv", "src/before.ts", "src/after.ts");
    git(root, "commit", "-qm", "rename");
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(0);
  });

  test("fails closed when the base ref does not resolve", async () => {
    const { root } = repo({}, { "src/a.ts": "export const a = 1;\n" });
    for (const bad of ["no-such-ref", "0000000000000000000000000000000000000000"]) {
      const r = await lint(root, bad);
      expect(r.status, `${bad}: ${r.stdout}`).not.toBe(0);
    }
  });

  test("exits 2 when no base ref is given", async () => {
    const { root } = repo({}, {});
    const r = await lint(root);
    expect(r.status, r.stdout + r.stderr).toBe(2);
  });

  test("flags a comment added to a path with a space", async () => {
    const { root, base } = repo({}, { "src/sp ace/b.ts": "export const a = 1;\n// note\n" });
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(1);
    expect(r.stdout).toContain("src/sp ace/b.ts:2 ");
  });

  test("exits 2 when the base looks like a git option", async () => {
    const { root } = repo({}, { "src/a.ts": "export const a = 1;\n" });
    for (const bad of ["--", "--stat", "--output=/tmp/lint-changed-comments-opt"]) {
      const r = await lint(root, bad);
      expect(r.status, `${bad}: ${r.stdout}${r.stderr}`).toBe(2);
    }
  });

  test("exits 2 when the base ref cannot be resolved", async () => {
    const { root } = repo({}, { "src/a.ts": "export const a = 1;\n" });
    for (const bad of ["no-such-ref", "0000000000000000000000000000000000000000"]) {
      const r = await lint(root, bad);
      expect(r.status, `${bad}: ${r.stdout}${r.stderr}`).toBe(2);
    }
  });

  test("does not flag a comment moved into a new file", async () => {
    const { root, base } = repo({ "src/before.ts": "// kept\nexport const a = 1;\n" }, {});
    rmSync(join(root, "src/before.ts"));
    write(root, { "src/moved-into/after.ts": "// kept\nexport const a = 1;\n" });
    git(root, "add", "-A");
    git(root, "commit", "-qm", "move");
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(0);
  });

  test("flags a new comment in a brand-new file", async () => {
    const { root, base } = repo({}, { "src/brand-new.ts": "// brand new\nexport const a = 1;\n" });
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(1);
    expect(r.stdout).toContain("src/brand-new.ts:1 ");
  });
});

describe.concurrent("lint-changed-comments on hostile paths and bases", { timeout: 60000 }, () => {
  test("never lets an option-shaped base write a file", async () => {
    const { existsSync } = await import("node:fs");
    const { root } = repo({}, { "src/a.ts": "export const a = 1;\n" });
    const out = join(root, "written-by-git");
    const r = await lint(root, `--output=${out}`);
    expect(r.status, r.stdout + r.stderr).toBe(2);
    expect(existsSync(out)).toBe(false);
  });

  test("exits 2 when the base is a single-dash option", async () => {
    const { root } = repo({}, { "src/a.ts": "export const a = 1;\n// note\n" });
    for (const bad of ["-", "-R", "-p", "-Snote"]) {
      const r = await lint(root, bad);
      expect(r.status, `${bad}: ${r.stdout}${r.stderr}`).toBe(2);
    }
  });

  test("exits 2 when the base is a range, a tree or a blob rather than one commit", async () => {
    const { root, base } = repo({}, { "src/a.ts": "export const a = 1;\n// note\n" });
    for (const bad of [`${base}..HEAD`, "HEAD...HEAD", "HEAD..HEAD", "HEAD^{tree}", "HEAD:src/a.ts"]) {
      const r = await lint(root, bad);
      expect(r.status, `${bad}: ${r.stdout}${r.stderr}`).toBe(2);
    }
  });

  test("flags a comment added to a file renamed in the same change", async () => {
    const body = "export const a = 1;\nexport const b = 2;\nexport const c = 3;\nexport const d = 4;\nexport const e = 5;\n";
    const { root, base } = repo({ "src/old name.ts": body }, {});
    rmSync(join(root, "src/old name.ts"));
    write(root, { "src/new name.ts": `${body}// note\n` });
    git(root, "add", "-A");
    git(root, "commit", "-qm", "rename with edit");
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(1);
    expect(r.stdout).toContain("src/new name.ts:6 ");
  });

  test("flags a comment added to a copy of an unchanged file", async () => {
    const body = "// kept\nexport const a = 1;\nexport const b = 2;\nexport const c = 3;\nexport const d = 4;\n";
    const { root, base } = repo({ "src/source.ts": body }, { "src/copy.ts": `${body}// added\n` });
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(1);
    expect(r.stdout).toContain("src/copy.ts:6 ");
    expect(r.stdout).not.toContain("src/copy.ts:1 ");
  });

  test("does not flag the comments of an unchanged file copied as is", async () => {
    const body = "// kept\nexport const a = 1;\nexport const b = 2;\nexport const c = 3;\nexport const d = 4;\n";
    const { root, base } = repo({ "src/source.ts": body }, { "src/copy.ts": body });
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(0);
  });

  test("flags a new file whose comment repeats the text of an unrelated base comment", async () => {
    const { root, base } = repo(
      { "src/old.ts": "// note\nexport const a = 1;\n" },
      { "src/other.ts": "export const z = 99;\n// note\nexport const y = 98;\n" },
    );
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(1);
    expect(r.stdout).toContain("src/other.ts:2 ");
  });

  test("passes a change that only deletes a file, even one with a space in its path", async () => {
    const { root, base } = repo({ "src/go ne.ts": "// old\nexport const a = 1;\n" }, {});
    git(root, "rm", "-q", "src/go ne.ts");
    git(root, "commit", "-qm", "delete");
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(0);
  });

  test("passes an empty diff", async () => {
    const { root } = repo({ "src/a.ts": "// old\nexport const a = 1;\n" }, {});
    const r = await lint(root, "HEAD");
    expect(r.status, r.stdout + r.stderr).toBe(0);
  });

  test("flags a comment against a base with no common ancestor", async () => {
    const { root } = repo({}, {});
    const branch = git(root, "rev-parse", "--abbrev-ref", "HEAD");
    git(root, "checkout", "-q", "--orphan", "unrelated");
    git(root, "rm", "-rqf", ".");
    write(root, { "src/keep.ts": "export const keep = 0;\n" });
    git(root, "add", "-A");
    git(root, "commit", "-qm", "unrelated base");
    const orphan = git(root, "rev-parse", "HEAD");
    git(root, "checkout", "-q", branch);
    write(root, { "src/a.ts": "export const a = 1;\n// note\n" });
    git(root, "add", "-A");
    git(root, "commit", "-qm", "change");
    const r = await lint(root, orphan);
    expect(r.status, r.stdout + r.stderr).toBe(1);
    expect(r.stdout).toContain("src/a.ts:2 ");
  });

  test("flags a comment added to a tracked file but not yet committed", async () => {
    const { root, base } = repo({ "src/a.ts": "export const a = 1;\n" }, {});
    write(root, { "src/a.ts": "export const a = 1;\n// note\n" });
    const r = await lint(root, base);
    expect(r.status, r.stdout + r.stderr).toBe(1);
    expect(r.stdout).toContain("src/a.ts:2 ");
  });
});
