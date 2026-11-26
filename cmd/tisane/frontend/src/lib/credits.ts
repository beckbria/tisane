declare const __DICTIONARY_CREDITS__: string;

export const DICTIONARY_CREDITS_MD: string = __DICTIONARY_CREDITS__;

export type CreditsInline =
  | { kind: 'text'; text: string }
  | { kind: 'link'; label: string; url: string };

export type CreditsBlock =
  | { kind: 'heading'; level: number; segments: CreditsInline[] }
  | { kind: 'paragraph'; lines: CreditsInline[][] }
  | { kind: 'list'; items: CreditsInline[][] };

const INLINE_LINK_RE = /\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)|(https?:\/\/[^\s<>()]+)/g;
const TRAILING_PUNCT_RE = /[.,;:!?]+$/;

function parseInline(line: string): CreditsInline[] {
  const segments: CreditsInline[] = [];
  let cursor = 0;
  INLINE_LINK_RE.lastIndex = 0;

  let match: RegExpExecArray | null;
  while ((match = INLINE_LINK_RE.exec(line)) !== null) {
    if (match.index > cursor) {
      segments.push({ kind: 'text', text: line.slice(cursor, match.index) });
    }

    if (match[1] !== undefined && match[2] !== undefined) {
      segments.push({ kind: 'link', label: match[1], url: match[2] });
      cursor = match.index + match[0].length;
    } else if (match[3] !== undefined) {
      const rawUrl = match[3];
      const trimmedUrl = rawUrl.replace(TRAILING_PUNCT_RE, '');
      const trailing = rawUrl.slice(trimmedUrl.length);
      if (trimmedUrl.length > 0) {
        segments.push({ kind: 'link', label: trimmedUrl, url: trimmedUrl });
      }
      if (trailing.length > 0) {
        segments.push({ kind: 'text', text: trailing });
      }
      cursor = match.index + match[0].length;
    }
  }

  if (cursor < line.length) {
    segments.push({ kind: 'text', text: line.slice(cursor) });
  }
  return segments;
}

/**
 * Parses a lightweight subset of Markdown (headings, paragraphs with preserved
 * line breaks, unordered lists, Markdown links, and bare http(s) URLs) into
 * structured blocks so credits render natively without {@html}.
 */
export function parseCreditsMarkdown(raw: string): CreditsBlock[] {
  const normalized = raw.replace(/\r\n?/g, '\n');
  const lines = normalized.split('\n');
  const blocks: CreditsBlock[] = [];

  let paragraphLines: CreditsInline[][] = [];
  let listItems: CreditsInline[][] = [];

  function flushParagraph() {
    if (paragraphLines.length > 0) {
      blocks.push({ kind: 'paragraph', lines: paragraphLines });
      paragraphLines = [];
    }
  }

  function flushList() {
    if (listItems.length > 0) {
      blocks.push({ kind: 'list', items: listItems });
      listItems = [];
    }
  }

  for (const rawLine of lines) {
    const trimmed = rawLine.trim();
    if (trimmed === '') {
      flushParagraph();
      flushList();
      continue;
    }

    const headingMatch = /^(#{1,6})\s+(.+)$/.exec(trimmed);
    if (headingMatch) {
      flushParagraph();
      flushList();
      blocks.push({
        kind: 'heading',
        level: headingMatch[1].length,
        segments: parseInline(headingMatch[2].trim())
      });
      continue;
    }

    const listMatch = /^[-*]\s+(.+)$/.exec(trimmed);
    if (listMatch) {
      flushParagraph();
      listItems.push(parseInline(listMatch[1].trim()));
      continue;
    }

    flushList();
    paragraphLines.push(parseInline(trimmed));
  }

  flushParagraph();
  flushList();

  return blocks;
}
