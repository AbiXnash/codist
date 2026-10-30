import { createHighlighterCore, type HighlighterCore, type ThemedToken } from 'shiki/core';
import { createJavaScriptRegexEngine } from 'shiki/engine/javascript';

const THEMES = { dark: 'github-dark-default', light: 'github-light-default' } as const;
const LANGS: Record<string, string> = {
  java: 'java', xml: 'xml', yml: 'yaml', yaml: 'yaml', properties: 'properties',
  sql: 'sql', kts: 'kotlin', gradle: 'groovy', json: 'json',
};

let highlighter: Promise<HighlighterCore> | null = null;

// Only the languages a Spring Boot repo needs are bundled.
function get() {
  highlighter ??= createHighlighterCore({
    themes: [import('@shikijs/themes/github-dark-default'), import('@shikijs/themes/github-light-default')],
    langs: [
      import('@shikijs/langs/java'),
      import('@shikijs/langs/xml'),
      import('@shikijs/langs/yaml'),
      import('@shikijs/langs/properties'),
      import('@shikijs/langs/sql'),
      import('@shikijs/langs/kotlin'),
      import('@shikijs/langs/groovy'),
      import('@shikijs/langs/json'),
    ],
    engine: createJavaScriptRegexEngine(),
  });
  return highlighter;
}

export function langFor(path: string): string | null {
  return LANGS[path.slice(path.lastIndexOf('.') + 1).toLowerCase()] ?? null;
}

export type Line = ThemedToken[];

/** Tokenises code into lines of coloured tokens. Unknown languages come back as plain text. */
export async function tokenize(code: string, lang: string | null, theme: keyof typeof THEMES = 'dark'): Promise<Line[]> {
  if (!lang) return plain(code);
  try {
    const h = await get();
    return h.codeToTokensBase(code, { lang, theme: THEMES[theme] });
  } catch {
    return plain(code);
  }
}

export function plain(code: string): Line[] {
  return code.split('\n').map((content) => [{ content, offset: 0 } as ThemedToken]);
}

/** Warm the highlighter up while the app boots. */
export function preload() {
  void get();
}
