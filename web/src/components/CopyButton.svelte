<script lang="ts">
  import { toast } from '../lib/store.svelte';

  // Copies text, with a tick for a moment to show it worked. With a label it reads as a button.
  let { text, label = '', title = 'Copy' }: { text: string | (() => string); label?: string; title?: string } = $props();
  let done = $state(false);
  let timer: ReturnType<typeof setTimeout>;
  async function copy(e: Event) {
    e.stopPropagation();
    e.preventDefault();
    try {
      await navigator.clipboard.writeText(typeof text === 'function' ? text() : text);
      done = true;
      clearTimeout(timer);
      timer = setTimeout(() => (done = false), 1200);
    } catch {
      toast('The browser blocked copying', 'warn');
    }
  }
</script>

<button type="button" class="copy" class:labelled={!!label} class:done {title} aria-label={title} onclick={copy}>
  {#if done}
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m3 8.5 3.2 3.2L13 5"/></svg>
  {:else}
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"><rect x="5.5" y="5.5" width="8" height="8" rx="1.5"/><path d="M10.5 5.5V4A1.5 1.5 0 0 0 9 2.5H4A1.5 1.5 0 0 0 2.5 4v5A1.5 1.5 0 0 0 4 10.5h1.5"/></svg>
  {/if}
  {#if label}<span>{done ? 'Copied' : label}</span>{/if}
</button>

<style>
  .copy { display: inline-flex; align-items: center; gap: 5px; flex-shrink: 0; height: 22px; min-width: 22px; justify-content: center; padding: 0 3px; border: 0; border-radius: var(--r); background: none; color: var(--text-3); font-size: 11.5px; cursor: pointer; }
  .copy:hover { color: var(--text); background: var(--hover); }
  .copy.labelled { padding: 0 7px; border: 1px solid var(--border); }
  .copy.done { color: var(--good); }
</style>
