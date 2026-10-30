<script lang="ts">
  import { app, proposeGuidedFix } from '../lib/store.svelte';
  import type { FindingWithPath } from '../lib/types';

  // A suggestion for how to fix THIS issue. The server refuses anything about other issues or code.
  let { finding, sha }: { finding: FindingWithPath; sha: string } = $props();
  let open = $state(false);
  let text = $state('');
  let busy = $state(false);
  let refused = $state<string | null>(null);

  async function send() {
    if (!text.trim() || busy) return;
    busy = true;
    refused = null;
    refused = await proposeGuidedFix(finding, sha, text);
    busy = false;
    if (!refused) { text = ''; open = false; }
  }
</script>

{#if !open}
  <button class="link" onclick={(e) => { e.stopPropagation(); open = true; }} disabled={!app.status?.canFix}>Suggest how to fix it…</button>
{:else}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="suggest" onclick={(e) => e.stopPropagation()}>
    <label class="label" for="sg-{finding.path}-{finding.line}">Your suggestion for this fix</label>
    <textarea id="sg-{finding.path}-{finding.line}" rows="3" bind:value={text} disabled={busy}
      placeholder={`e.g. "use constructor injection and keep the field name"`}
      onkeydown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) void send(); if (e.key === 'Escape') open = false; }}></textarea>
    <p class="faint small">Only about “{finding.title}”. Suggestions about other issues, files or features are refused.</p>
    {#if refused}<p class="refused" role="alert">{refused}</p>{/if}
    <div class="row">
      <button class="btn sm" onclick={send} disabled={busy || !text.trim()}>
        {#if busy}<span class="spinner"></span>Checking and writing the fix…{:else}Fix with my suggestion{/if}
      </button>
      <button class="btn secondary sm" onclick={() => { open = false; refused = null; }} disabled={busy}>Cancel</button>
      <span class="faint small kbd">⌘↵</span>
    </div>
  </div>
{/if}

<style>
  .link { border: 0; background: none; padding: 0; color: var(--accent-text); font-size: 12px; }
  .link:hover:not(:disabled) { text-decoration: underline; }
  .link:disabled { opacity: 0.5; cursor: default; }
  .suggest { display: grid; gap: 6px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); width: 100%; }
  textarea { width: 100%; resize: vertical; padding: 8px 10px; border: 1px solid var(--border-strong); border-radius: var(--r); background: var(--surface); color: var(--text); font: inherit; font-size: 12.5px; }
  textarea:focus { outline: none; border-color: var(--accent); }
  .small { margin: 0; font-size: 11.5px; }
  .refused { margin: 0; padding: 7px 10px; border-radius: var(--r); font-size: 12px; color: var(--text); background: color-mix(in srgb, var(--bad) 12%, transparent); border: 1px solid color-mix(in srgb, var(--bad) 35%, transparent); }
  .row { display: flex; align-items: center; gap: 6px; }
  .kbd { margin-left: auto; }
</style>
