import js from "@eslint/js";
import globals from "globals";
import jsxA11y from "eslint-plugin-jsx-a11y";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

const unsafeHtml = {
  selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
  message: "dangerouslySetInnerHTML is banned. Render text through React.",
};

const inlineStyle = {
  selector: "JSXAttribute[name.name='style']",
  message: "Inline style={} is banned. Use Tailwind classes and theme tokens.",
};

export default tseslint.config(
  { ignores: ["dist", "../internal/webui/dist"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  reactHooks.configs["recommended-latest"],
  jsxA11y.flatConfigs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2022,
      globals: { ...globals.browser },
    },
    rules: {
      "no-restricted-syntax": ["error", unsafeHtml],
    },
  },
  {
    files: ["src/components/**/*.{ts,tsx}", "src/ui/**/*.{ts,tsx}", "src/charts/**/*.{ts,tsx}", "src/panels/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-syntax": ["error", unsafeHtml, inlineStyle],
    },
  },
);
