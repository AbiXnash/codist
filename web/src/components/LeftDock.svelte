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
  .tabs { display: flex; gap: 0; margin: 10px; padding: 2px; border-radius: var(--r); background: var(--surface-3); flex-shrink: 0; }
  .tabs button { flex: 1; height: 24px; border: 0; border-radius: 5px; background: none; color: var(--text); font-size: 12.5px; }
    .tabs button.on { font-weight: 500; background: var(--surface-2); box-shadow: var(--raised); }
  .n { font-size: 11px; color: var(--text-3); font-variant-numeric: tabular-nums; }
  .body { flex: 1; min-height: 0; overflow: auto; display: flex; flex-direction: column; }
  @media (max-width: 1000px) { .dock { max-height: 420px; border-right: 0; border-bottom: 1px solid var(--border); } }
</style>
