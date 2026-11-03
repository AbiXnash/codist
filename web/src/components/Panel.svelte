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
  // Three places: what is wrong, the commit you picked, the code. Ask stands apart.
  const tabs = $derived<{ id: Tab; label: string; count?: number }[]>([
    ...(app.panelOn('issues') ? [{ id: 'issues' as Tab, label: 'Issues', count: open }] : []),
    ...(app.panelOn('commit') ? [{ id: 'review' as Tab, label: 'Commit', count: app.issues.length }] : []),
    ...(app.panelOn('code') ? [{ id: 'code' as Tab, label: 'Code' }] : []),
    ...(app.selectedNode ? [{ id: 'type' as Tab, label: 'Class' }] : []),
  ]);
  const onCommit = $derived(app.tab === 'review' || app.tab === 'diff');
  // On Review the centre already shows the changes, so the panel needn't.
  $effect(() => {
    if (app.mode === 'review' && app.tab === 'diff') app.tab = 'review';
  });
</script>

<section class="panel">
  <div class="tabs">
    <div class="pills" role="tablist">
    {#each tabs as t, i (t.id)}
      <button role="tab" aria-selected={app.tab === t.id} class:active={app.tab === t.id || (t.id === 'review' && onCommit)} onclick={() => (app.tab = t.id)} title="{t.label}  {i + 1}">
        {t.label}
        {#if t.count}<span class="count" class:hot={t.id === 'issues' && hot}>{t.count}</span>{/if}
      </button>
    {/each}
    </div>
    <span class="spacer"></span>
    {#if app.panelOn('ask')}<button class="ask" class:on={app.tab === 'ask'} title="Ask about the code, or describe a change" onclick={() => (app.tab = app.tab === 'ask' ? 'issues' : 'ask')}>Ask AI</button>{/if}
    <button class="icon-btn" title="Hide panel  ]" aria-label="Hide panel" onclick={() => (app.showPanel = false)}>
      <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="m4 4 8 8M12 4l-8 8"/></svg>
    </button>
  </div>

  {#if onCommit && app.mode !== 'review'}
    <div class="sub">
      <div class="seg">
        <button class:on={app.tab === 'review'} onclick={() => (app.tab = 'review')}>Review{#if app.issues.length} · {app.issues.length}{/if}</button>
        <button class:on={app.tab === 'diff'} onclick={() => (app.tab = 'diff')}>Changes{#if app.detail?.analysis.files.length} · {app.detail.analysis.files.length}{/if}</button>
      </div>
    </div>
  {/if}
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
  .tabs { display: flex; align-items: center; gap: 6px; height: 48px; padding: 0 8px 0 10px; border-bottom: 1px solid var(--border); flex-shrink: 0; }
  .pills { display: flex; padding: 2px; border-radius: var(--r); background: var(--surface-3); }
  .pills button { display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 11px; border: 0; border-radius: 5px; background: none; color: var(--text); font-size: 12.5px; }
  .pills button.active { background: var(--surface-2); box-shadow: var(--raised); font-weight: 500; }
  .count { min-width: 18px; padding: 0 5px; border-radius: 9px; font-size: 11px; line-height: 17px; text-align: center; background: var(--surface-3); color: var(--text-2); }
  .count.hot { background: color-mix(in srgb, var(--sev-critical) 18%, transparent); color: var(--sev-critical); }
  .spacer { flex: 1; }
  .ask { white-space: nowrap; height: 28px; margin-right: 4px; padding: 0 10px; border: 0; border-radius: var(--r); background: var(--surface-3); color: var(--text); font-size: 12.5px; }
  .ask.on { color: #fff; background: var(--accent); }
  .sub { padding: 7px 10px; border-bottom: 1px solid var(--border); flex-shrink: 0; }
  .seg { display: inline-flex; padding: 2px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .seg button { height: 24px; padding: 0 12px; border: 0; border-radius: var(--r); background: none; font-size: 12px; color: var(--text-3); }
  .seg button.on { color: var(--text); background: var(--surface-3); box-shadow: var(--raised); }
  .body { flex: 1; min-height: 0; overflow: auto; }
  @media (max-width: 1000px) { .panel { min-height: 70vh; border-top: 1px solid var(--border); } }
</style>
