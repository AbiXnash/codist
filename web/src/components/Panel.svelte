<script lang="ts">
  import { app } from '../lib/store.svelte';
  import type { Tab } from '../lib/types';
  import IssuesHub from './IssuesHub.svelte';
  import ReviewTab from './ReviewTab.svelte';
  import CodeTab from './CodeTab.svelte';
  import DiffTab from './DiffTab.svelte';
  import TypeTab from './TypeTab.svelte';
  import AskTab from './AskTab.svelte';

  const open = $derived((app.issueList?.issues ?? []).filter((i) => i.state === 'open' && (!i.group || i.groupRoot)).length);
  const hot = $derived((app.issueList?.openBySeverity.critical ?? 0) + (app.issueList?.openBySeverity.high ?? 0) > 0);
  const tabs = $derived<{ id: Tab; label: string; count?: number }[]>([
    { id: 'issues', label: 'Issues', count: open },
    { id: 'review', label: 'Commit', count: app.issues.length },
    { id: 'diff', label: 'Diff', count: app.detail?.analysis.files.length },
    { id: 'code', label: 'Code' },
    ...(app.selectedNode ? [{ id: 'type' as Tab, label: 'Type' }] : []),
    { id: 'ask', label: 'Ask' },
  ]);
</script>

<section class="panel">
  <div class="tabs" role="tablist">
    {#each tabs as t, i (t.id)}
      <button role="tab" aria-selected={app.tab === t.id} class:active={app.tab === t.id} onclick={() => (app.tab = t.id)} title="{t.label}  {i + 1}">
        {t.label}
        {#if t.count}<span class="count" class:hot={t.id === 'issues' && hot}>{t.count}</span>{/if}
      </button>
    {/each}
    <span class="spacer"></span>
    <button class="icon-btn" title="Hide panel  ]" aria-label="Hide panel" onclick={() => (app.showPanel = false)}>
      <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="m4 4 8 8M12 4l-8 8"/></svg>
    </button>
  </div>

  <div class="body">
    {#if app.tab === 'issues'}<IssuesHub />
    {:else if app.tab === 'review'}<ReviewTab />
    {:else if app.tab === 'code'}<CodeTab />
    {:else if app.tab === 'diff'}<DiffTab />
    {:else if app.tab === 'type'}<TypeTab />
    {:else}<AskTab />{/if}
  </div>
</section>

<style>
  .panel { display: flex; flex-direction: column; min-width: 0; min-height: 0; background: var(--surface); }
  .tabs { display: flex; align-items: center; gap: 2px; height: 40px; padding: 0 6px 0 10px; border-bottom: 1px solid var(--border); flex-shrink: 0; }
  .tabs > button:not(.icon-btn) {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 40px;
    padding: 0 10px;
    border: 0;
    background: none;
    color: var(--text-3);
    font-weight: 500;
  }
  .tabs > button:not(.icon-btn):hover { color: var(--text); }
  .tabs > button.active { color: var(--text); }
  .tabs > button.active::after { content: ''; position: absolute; left: 8px; right: 8px; bottom: -1px; height: 2px; border-radius: 2px; background: var(--accent); }
  .count { min-width: 18px; padding: 0 5px; border-radius: 9px; font-size: 11px; line-height: 17px; text-align: center; background: var(--surface-3); color: var(--text-2); }
  .count.hot { background: color-mix(in srgb, var(--sev-critical) 18%, transparent); color: var(--sev-critical); }
  .spacer { flex: 1; }
  .body { flex: 1; min-height: 0; overflow: auto; }
  @media (max-width: 1000px) { .panel { min-height: 70vh; border-top: 1px solid var(--border); } }
</style>
