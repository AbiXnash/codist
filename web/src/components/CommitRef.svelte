<script lang="ts">
  import { app, selectCommit } from '../lib/store.svelte';
  let { sha }: { sha: string } = $props();
  const known = $derived(app.commits.some((c) => c.commit.sha === sha));
</script>

<button class="ref mono" disabled={!known} title={known ? 'Open this commit' : sha} onclick={(e) => { e.stopPropagation(); void selectCommit(sha, true); app.tab = 'review'; }}>{sha.slice(0, 7)}</button>

<style>
  .ref { padding: 0 4px; border: 0; border-radius: 3px; background: var(--surface-3); color: var(--accent-text); font-size: 11px; cursor: pointer; }
  .ref:hover:not(:disabled) { text-decoration: underline; }
  .ref:disabled { cursor: default; color: var(--text-2); }
</style>
