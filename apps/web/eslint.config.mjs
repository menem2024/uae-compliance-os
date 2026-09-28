import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

/*
 * RTL guard (adr/016): physical direction utilities break Arabic layouts.
 * Flags ml-/mr-/pl-/pr-/left-/right-/text-left/text-right/rounded-l/r/border-l/r
 * in class strings: JSX className values (including inside cn()/ternaries/
 * templates) and cn()/cva() arguments anywhere.
 */
// The brief's pattern, plus the bare forms it misses (`text-left`, `border-l`, `rounded-r`, `rounded-tl-*`).
const PHYSICAL = String.raw`/\b(ml|mr|pl|pr|left|right|text-left|text-right|rounded-l|rounded-r|border-l|border-r)-|\b(text-left|text-right|rounded-[lr]|border-[lr])\b|\brounded-[tb][lr]\b/`;
const message = "use logical utilities (ms/me/ps/pe/start/end)";
const rtlRule = [
  "error",
  { selector: `JSXAttribute[name.name="className"] Literal[value=${PHYSICAL}]`, message },
  { selector: `JSXAttribute[name.name="className"] TemplateElement[value.raw=${PHYSICAL}]`, message },
  { selector: `CallExpression[callee.name=/^(cn|cva|clsx)$/] Literal[value=${PHYSICAL}]`, message },
];

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    files: ["**/*.{ts,tsx}"],
    rules: { "no-restricted-syntax": rtlRule },
  },
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
  ]),
]);

export default eslintConfig;
