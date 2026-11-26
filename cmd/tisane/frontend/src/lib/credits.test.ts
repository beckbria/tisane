import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { DICTIONARY_CREDITS_MD, parseCreditsMarkdown } from './credits';

describe('credits', () => {
  it('embeds dictionaries/CREDITS.md verbatim', () => {
    const expected = readFileSync(resolve(process.cwd(), '../../../dictionaries/CREDITS.md'), 'utf8');
    expect(DICTIONARY_CREDITS_MD).toBe(expected);
  });

  it('parses headings, multi-line paragraphs, markdown links, bare URLs, and lists', () => {
    const sample = [
      '# Dictionary credits\r\n',
      'Intro with [Example](https://example.com/).\r\n',
      '## Source One\r\n',
      'Line one (https://example.org/path)\r\nLine two https://example.net/doc.\r\n',
      '- Bullet item\r\n- Second [link](http://example.com/a)\r\n'
    ].join('\n');

    const blocks = parseCreditsMarkdown(sample);
    expect(blocks).toEqual([
      {
        kind: 'heading',
        level: 1,
        segments: [{ kind: 'text', text: 'Dictionary credits' }]
      },
      {
        kind: 'paragraph',
        lines: [
          [
            { kind: 'text', text: 'Intro with ' },
            { kind: 'link', label: 'Example', url: 'https://example.com/' },
            { kind: 'text', text: '.' }
          ]
        ]
      },
      {
        kind: 'heading',
        level: 2,
        segments: [{ kind: 'text', text: 'Source One' }]
      },
      {
        kind: 'paragraph',
        lines: [
          [
            { kind: 'text', text: 'Line one (' },
            { kind: 'link', label: 'https://example.org/path', url: 'https://example.org/path' },
            { kind: 'text', text: ')' }
          ],
          [
            { kind: 'text', text: 'Line two ' },
            { kind: 'link', label: 'https://example.net/doc', url: 'https://example.net/doc' },
            { kind: 'text', text: '.' }
          ]
        ]
      },
      {
        kind: 'list',
        items: [
          [{ kind: 'text', text: 'Bullet item' }],
          [
            { kind: 'text', text: 'Second ' },
            { kind: 'link', label: 'link', url: 'http://example.com/a' }
          ]
        ]
      }
    ]);
  });

  it('does not turn non-http(s) markdown links into clickable links', () => {
    const blocks = parseCreditsMarkdown('Do not click [bad](javascript:alert(1)).');
    expect(blocks).toEqual([
      {
        kind: 'paragraph',
        lines: [[{ kind: 'text', text: 'Do not click [bad](javascript:alert(1)).' }]]
      }
    ]);
  });
});
