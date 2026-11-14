import tseslint from "typescript-eslint";

export default [
  { ignores: [".next/**", "lib/api/schema.d.ts", "next-env.d.ts"] },
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      globals: {
        console: "readonly",
        document: "readonly",
        fetch: "readonly",
        localStorage: "readonly",
        process: "readonly",
        RequestInit: "readonly",
        window: "readonly",
      },
    },
	rules: {
	  "no-undef": "off",
	},
  },
];
