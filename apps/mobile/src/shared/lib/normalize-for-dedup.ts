/**
 * 8-step normalization for dedup matching -- parity with backend normalize_for_match.
 */

const BRACKET_SUFFIX_RE = /[\(\[\{][^\)\]\}]*[\)\]\}]/g;
const FEATURE_TOKEN_RE = /\b(feat\.?|ft\.?|featuring|with)\b/gi;
const LEADING_ARTICLE_RE = /^\s*(the|los|les|el|la|le)\s+/i;
const PUNCT_RE = /[^\w\s]/g;
const WHITESPACE_RE = /\s+/g;
// Build from char codes to avoid encoding issues with curly quotes
const APOSTROPHE_CHARS = "'.,'" + String.fromCharCode(0x2018, 0x2019);
const APOSTROPHE_RE = new RegExp("[" + APOSTROPHE_CHARS + "]", "g");

function stripDiacritics(s: string): string {
  return s.normalize('NFD').replace(/[̀-ͯ]/g, '');
}

function stripLeadingArticle(s: string): string {
  const stripped = s.replace(LEADING_ARTICLE_RE, '');
  if (stripped.trim() && stripped !== s) return stripped;
  return s;
}

export function normalizeForDedup(text: string): string {
  let s = text.normalize('NFKC');
  s = s.toLowerCase();
  s = stripDiacritics(s);
  s = s.replace(FEATURE_TOKEN_RE, 'feat');
  s = s.replace(BRACKET_SUFFIX_RE, ' ');
  s = stripLeadingArticle(s);
  s = s.replace(/&/g, ' and ');
  s = s.replace(APOSTROPHE_RE, '');
  s = s.replace(PUNCT_RE, ' ');
  s = s.replace(WHITESPACE_RE, ' ');
  return s.trim();
}