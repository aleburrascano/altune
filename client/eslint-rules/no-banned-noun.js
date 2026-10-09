const BANNED_WORD = /^songs?$/i;

const wordsOf = (text) =>
  text
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1 $2')
    .split(/[^A-Za-z]+/)
    .filter(Boolean);

const bannedWordIn = (text) => wordsOf(text).find((word) => BANNED_WORD.test(word));

module.exports = {
  meta: {
    type: 'problem',
    messages: {
      bannedNoun: "'{{word}}' is banned; the noun is Track.",
    },
    schema: [],
  },
  create(context) {
    const check = (node, text) => {
      if (typeof text !== 'string') return;
      const word = bannedWordIn(text);
      if (word) context.report({ node, messageId: 'bannedNoun', data: { word } });
    };
    return {
      Identifier(node) {
        check(node, node.name);
      },
      Literal(node) {
        check(node, node.value);
      },
      TemplateElement(node) {
        check(node, node.value.cooked ?? node.value.raw);
      },
    };
  },
};
