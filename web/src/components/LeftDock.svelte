<script lang="ts">
  import { app } from '../lib/store.svelte';
  import CommitsTab from './CommitsTab.svelte';
  import GraphPanel from './GraphPanel.svelte';
</script>

<aside class="dock">
  <div class="tabs" role="tablist">
    <button role="tab" aria-selected={app.left === 'commits'} class:on={app.left === 'commits'} onclick={() => (app.left = 'commits')}>Commits <span class="n">{app.shownCommits.length}</span></button>
    {#if app.featureOn('explorer')}<button role="tab" aria-selected={app.left === 'explorer'} class:on={app.left === 'explorer'} onclick={() => (app.left = 'explorer')}>Explorer <span class="n">{app.graphView.nodes.length}</span></button>{/if}
  </div>
  <div class="body">
    {#if app.left === 'commits'}<CommitsTab />{:else}<GraphPanel />{/if}
  </div>
</aside>

<style>
  .dock { display: flex; flex-direction: column; min-height: 0; min-width: 0; border-right: 1px solid var(--border); background: var(--surface); }
  .tabs { display: flex; gap: 2px; padding: 6px 8px; border-bottom: 1px solid var(--border); flex-shrink: 0; }
  .tabs button { flex: 1; height: 28px; border: 0; border-radius: var(--r); background: none; color: var(--text-3); font-weight: 500; }
  .tabs button:hover { color: var(--text); }
  .tabs button.on { color: var(--text); background: var(--surface-3); box-shadow: 0 0 0 1px var(--border-strong); }
  .n { font-size: 11px; color: var(--text-3); font-variant-numeric: tabular-nums; }
  .body { flex: 1; min-height: 0; overflow: auto; display: flex; flex-direction: column; }
  @media (max-width: 1000px) { .dock { max-height: 420px; border-right: 0; border-bottom: 1px solid var(--border); } }
</style>
