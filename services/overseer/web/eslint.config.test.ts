import { ESLint } from "eslint";
import { describe, expect, test } from "vitest";

const eslint = new ESLint();

async function noCommentsErrors(filePath: string, code: string) {
  const [result] = await eslint.lintText(code, { filePath });
  return result.messages.filter((m) => m.ruleId === "local/no-comments" && m.severity === 2);
}

describe("overseer web eslint config bans comments in every file", () => {
  test.each(["src/api.test.ts", "vite.config.ts"])("reports one error for a comment in %s", async (file) => {
    expect(await noCommentsErrors(file, "export const a = 1;\n// x\n")).toHaveLength(1);
  });

  test("reports nothing for a string that only looks like a comment", async () => {
    expect(await noCommentsErrors("src/api.ts", "export const url = 'https://x.dev';\n")).toHaveLength(0);
  });

  test("still reports a comment under a whole-file eslint-disable", async () => {
    const errors = await noCommentsErrors("src/api.ts", "/* eslint-disable */\nexport const a = 1;\n");
    expect(errors).toHaveLength(1);
  });
});
