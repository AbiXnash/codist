<script lang="ts">
  import { app, loadIssues } from '../lib/store.svelte';
  import CommitList from './CommitList.svelte';
  import IssuesView from './IssuesView.svelte';
  import Toggle from './Toggle.svelte';

  const open = $derived(app.issueList?.counts.open ?? 0);
  function show(v: 'commits' | 'issues') {
    app.sidebar = v;
    if (v === 'issues') void loadIssues();
  }
</script>

<aside class="side">
  <div class="head">
    <div class="seg" role="tablist">
      <button role="tab" aria-selected={app.sidebar === 'commits'} class:on={app.sidebar === 'commits'} onclick={() => show('commits')}>Commits</button>
      <button role="tab" aria-selected={app.sidebar === 'issues'} class:on={app.sidebar === 'issues'} onclick={() => show('issues')}>
        Issues {#if open}<span class="n" class:hot={(app.issueList?.openBySeverity.critical ?? 0) + (app.issueList?.openBySeverity.high ?? 0) > 0}>{open}</span>{/if}
      </button>
    </div>
    {#if app.sidebar === 'commits'}<Toggle bind:checked={app.follow} label="Follow" title="Select each new commit as it arrives" />{/if}
  </div>
  {#if app.sidebar === 'commits'}<CommitList />{:else}<IssuesView />{/if}
</aside>

<style>
  .side { display: flex; flex-direction: column; min-height: 0; border-right: 1px solid var(--border); background: var(--surface); }
  .head { display: flex; justify-content: space-between; align-items: center; gap: 8px; height: 40px; padding: 0 8px; border-bottom: 1px solid var(--border); flex-shrink: 0; }
  .seg { display: flex; padding: 2px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .seg button { display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 10px; border: 0; border-radius: var(--r); background: none; color: var(--text-3); font-weight: 500; font-size: 12.5px; }
  .seg button.on { color: var(--text); background: var(--surface-3); box-shadow: 0 0 0 1px var(--border-strong); }
  .n { min-width: 18px; padding: 0 5px; border-radius: 9px; font-size: 10.5px; line-height: 16px; text-align: center; background: var(--surface-3); color: var(--text-2); }
  .n.hot { background: color-mix(in srgb, var(--sev-critical) 18%, transparent); color: var(--sev-critical); }
  @media (max-width: 1000px) { .side { max-height: 320px; border-right: 0; border-bottom: 1px solid var(--border); } }
</style>
