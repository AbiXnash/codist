<script lang="ts">
  import { plain, tokenize, type Line } from '../lib/highlight';
  import { app, toast } from '../lib/store.svelte';

  let { code, lang = 'java' }: { code: string; lang?: string | null } = $props();

  let lines = $state<Line[]>([]);
  $effect(() => {
    const c = code, l = lang, theme = app.theme;
    lines = plain(c);
    if (/[;{}()=]/.test(c)) tokenize(c, l, theme).then((t) => { if (c === code && theme === app.theme) lines = t; });
  });

  function copy() {
    navigator.clipboard?.writeText(code).then(() => toast('Copied', 'good', 1500));
  }
</script>

<div class="block">
  <button class="copy icon-btn" title="Copy" aria-label="Copy" onclick={copy}>
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="5" y="5" width="9" height="9" rx="1.5"/><path d="M11 5V3.5A1.5 1.5 0 0 0 9.5 2h-6A1.5 1.5 0 0 0 2 3.5v6A1.5 1.5 0 0 0 3.5 11H5"/></svg>
  </button>
  <pre><code>{#each lines as line}<span>{#each line as t}<span style:color={t.color} class:i={t.fontStyle === 1}>{t.content}</span>{/each}</span>
{/each}</code></pre>
</div>

<style>
  .block { position: relative; }
  pre {
    margin: 0;
    padding: 8px 36px 8px 10px;
    overflow: auto;
    max-height: 280px;
    border: 1px solid var(--border);
    border-radius: var(--r);
    background: var(--code-bg);
    font-size: 12px;
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-word;
    color: var(--text);
  }
  .copy { position: absolute; top: 3px; right: 3px; width: 24px; height: 24px; }
  .i { font-style: italic; }
</style>
