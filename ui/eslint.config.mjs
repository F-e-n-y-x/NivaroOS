// @ts-check
import antfu from '@antfu/eslint-config'

// antfu's config, set to the style this codebase is actually written in
// (tabs, single quotes, no semicolons, `} else {`, inline template
// content, explicit closing tags), with the purely cosmetic rules the code
// doesn't follow consistently turned off. What's left is worth fixing.
export default antfu(
  {
    typescript: false,
    vue: true,
    unocss: false,
    markdown: true,
    stylistic: { indent: 'tab', quotes: 'single', semi: false },
    // Newer than this config's rule set; off to keep the rules as they were.
    e18e: false,
  },
  { files: ['**/*.json'], rules: { 'jsonc/indent': ['error', 2] } },
  {
    rules: {
      // Mixed across files (the service/ modules use semicolons and double
      // quotes); not worth a mass reformat.
      'style/semi': 'off',
      'style/quotes': 'off',
      'style/comma-dangle': 'off',
      'style/arrow-parens': 'off',
      'style/brace-style': ['error', '1tbs', { allowSingleLine: true }],
      'antfu/if-newline': 'off',
      'antfu/curly': 'off',
      'antfu/consistent-list-newline': 'off',
      'antfu/top-level-function': 'off',
      'prefer-template': 'off',
      'object-shorthand': 'off',
      'sort-imports': 'off',
      'perfectionist/sort-imports': 'off',
      'perfectionist/sort-named-imports': 'off',
      // main.js awaits the locale before mounting, on purpose.
      'antfu/no-top-level-await': 'off',
      'unicorn/number-literal-case': 'off',
      'unicorn/prefer-number-properties': 'off',
      'test/consistent-test-it': 'off',
      'test/prefer-lowercase-title': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/multiline-html-element-content-newline': 'off',
      'vue/html-self-closing': 'off',
      'vue/first-attribute-linebreak': 'off',
      'vue/html-closing-bracket-newline': 'off',
      'vue/order-in-components': 'off',
      'vue/block-order': 'off',
      'vue/attributes-order': 'off',
      'vue/prefer-template': 'off',
      'vue/component-name-in-template-casing': 'off',
      'vue/component-definition-name-casing': 'off',
      'vue/custom-event-name-casing': 'off',
      'vue/v-on-event-hyphenation': 'off',
    },
  },
)
