<script lang="ts">
  import { app, savePref, setTheme } from '../lib/store.svelte';
  import type { Mode } from '../lib/types';

  // Grouped the way a Mac source list is: what you are doing first, then the screen.
  const paths: Record<string, string> = {
    review: 'M8 2.5 13.5 5v3.2c0 3-2.2 4.8-5.5 5.8-3.3-1-5.5-2.8-5.5-5.8V5L8 2.5ZM5.8 8.1l1.6 1.6 2.9-3',
    repo: 'M3 3.5A1.5 1.5 0 0 1 4.5 2h7A1.5 1.5 0 0 1 13 3.5V14l-5-2.5L3 14V3.5Z',
    branches: 'M5 3.5v9M5 9c0-2.5 6-1.5 6-4.5M5 3.5a1.5 1.5 0 1 0 0-.01M11 3a1.5 1.5 0 1 0 0 .01M5 12.5a1.5 1.5 0 1 0 0 .01',
    mine: 'M8 8a2.75 2.75 0 1 0 0-5.5A2.75 2.75 0 0 0 8 8ZM2.8 13.8c.4-2.6 2.6-4 5.2-4s4.8 1.4 5.2 4',
    team: 'M6 7.5A2.25 2.25 0 1 0 6 3a2.25 2.25 0 0 0 0 4.5ZM1.8 13c.3-2.2 2-3.4 4.2-3.4s3.9 1.2 4.2 3.4M10.6 3.2a2.25 2.25 0 0 1 0 4.2M11.8 9.9c1.4.4 2.3 1.5 2.5 3.1',
    release: 'M9 2.2c2.5.2 4.6 2.3 4.8 4.8L9.6 11.2 4.8 6.4 9 2.2ZM6.3 4.9 3.4 5.3 2 6.7l2.8.6M11.1 9.7l-.4 2.9-1.4 1.4-.6-2.8M4 12c-.8.3-1.5 1-1.7 1.7.7-.2 1.4-.9 1.7-1.7',
    incidents: 'M8 2.4 14.2 13H1.8L8 2.4ZM8 6.5v3M8 11.2v.1',
    activity: 'M1.8 8.5h2.7L6.3 3.5l3.2 9 1.9-4h2.8',
    memory: 'M5.5 3.2A2.2 2.2 0 0 0 3.4 5.9 2.3 2.3 0 0 0 3 10a2.3 2.3 0 0 0 2.6 2.7c.6.9 2.4.9 2.4-.5V3.5c0-1.2-1.5-1.5-2.5-.3ZM10.5 3.2a2.2 2.2 0 0 1 2.1 2.7A2.3 2.3 0 0 1 13 10a2.3 2.3 0 0 1-2.6 2.7c-.6.9-2.4.9-2.4-.5',
  };
  const GROUPS: { title: string; items: [string, string][] }[] = [
    { title: 'Code', items: [['review', 'Review'], ['repo', 'Repository'], ['branches', 'Branches']] },
    { title: 'People', items: [['mine', 'My work'], ['team', 'Team']] },
    { title: 'Ship', items: [['release', 'Release'], ['incidents', 'Incidents']] },
    { title: 'AI', items: [['activity', 'Activity'], ['memory', 'Memory']] },
  ];
  const open = $derived((app.issueList?.issues ?? []).filter((i) => i.state === 'open' && (!i.group || i.groupRoot)).length);
  const badge = (m: string) => {
    if (m === 'review') return open || '';
    if (m === 'incidents') return app.incidents.filter((i) => i.status === 'analysing').length || '';
    return '';
  };
  function toggle() {
    app.showSidebar = !app.showSidebar;
    savePref('sidebar', app.showSidebar);
  }
</script>

<aside class="side" class:rail={!app.showSidebar} aria-label="Screens">
  <div class="brand">
    <button class="logo" title={app.showSidebar ? 'Collapse the sidebar  \\' : 'Expand the sidebar  \\'} aria-label="Toggle the sidebar" onclick={toggle}>
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="var(--accent)" stroke-width="2" stroke-linecap="round"><circle cx="6" cy="7" r="2.5"/><circle cx="18" cy="9" r="2.5"/><circle cx="10" cy="18" r="2.5"/><path d="m8.4 7.6 7.2 1M16.6 11 11.5 16M6.9 9.4 9.2 15.6"/></svg>
    </button>
    {#if app.showSidebar}<span>Codist</span>{/if}
  </div>

  <nav>
    {#each GROUPS.filter((g) => g.items.some(([m]) => app.menuOn(m))) as g}
      <div class="group">
        {#if app.showSidebar}<div class="h">{g.title}</div>{:else}<div class="gap"></div>{/if}
        {#each g.items.filter(([m]) => app.menuOn(m)) as [m, label]}
          <button class="item" class:on={app.mode === m} aria-current={app.mode === m ? 'page' : undefined} title={app.showSidebar ? undefined : label} onclick={() => (app.mode = m as Mode)}>
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d={paths[m]} /></svg>
            {#if app.showSidebar}<span class="t">{label}</span>{#if badge(m)}<span class="b">{badge(m)}</span>{/if}{:else if badge(m)}<i class="pip"></i>{/if}
          </button>
        {/each}
      </div>
    {/each}
  </nav>

  <footer>
    <div class="st" title={app.status?.llmProblem || `Review with ${app.status?.models?.review ?? 'the model'}`}>
      <span class="dot" style:background={app.status?.llmProblem ? 'var(--bad)' : app.live ? 'var(--good)' : 'var(--warn)'}></span>
      {#if app.showSidebar}<span class="m">{app.status?.llmProblem ? 'Rules only' : app.status?.model ?? 'Connecting'}</span>{/if}
    </div>
    <button class="icon-btn" title="Use the {app.theme === 'dark' ? 'light' : 'dark'} appearance" aria-label="Change appearance" onclick={() => setTheme(app.theme === 'dark' ? 'light' : 'dark')}>
      <svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4"><circle cx="8" cy="8" r="5.5"/><path d="M8 2.5a5.5 5.5 0 0 1 0 11Z" fill="currentColor" stroke="none"/></svg>
    </button>
  </footer>
</aside>

<style>
  .side { display: flex; flex-direction: column; width: 186px; flex-shrink: 0; min-height: 0; background: var(--sidebar); border-right: 1px solid var(--border); }
  .side.rail { width: 52px; }
  .brand { display: flex; align-items: center; gap: 8px; height: 48px; padding: 0 12px; font-size: 14px; font-weight: 600; flex-shrink: 0; }
  .logo { display: grid; place-items: center; width: 28px; height: 28px; padding: 0; border: 0; border-radius: var(--r); background: none; }
  .logo:hover { background: var(--hover); }
  nav { flex: 1; min-height: 0; overflow-y: auto; padding: 0 10px; }
  .group { margin-bottom: 14px; }
  .h { padding: 0 8px 4px; font-size: 11px; font-weight: 600; color: var(--text-3); }
  .gap { height: 1px; margin: 0 6px 8px; background: var(--border); }
  .group:first-child .gap { display: none; }
  .item { position: relative; display: flex; align-items: center; gap: 9px; width: 100%; height: 30px; margin-bottom: 1px; padding: 0 8px; border: 0; border-radius: var(--r); background: none; color: var(--text); font-size: 13px; text-align: left; }
  .item svg { flex-shrink: 0; color: var(--accent-text); }
  .item:hover { background: var(--hover); }
  .item.on { background: var(--accent); color: #fff; }
  .item.on svg { color: #fff; }
  .t { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .b { font-size: 12px; color: var(--text-2); font-variant-numeric: tabular-nums; }
  .item.on .b { color: rgba(255, 255, 255, 0.85); }
  .pip { position: absolute; top: 6px; right: 6px; width: 6px; height: 6px; border-radius: 50%; background: var(--bad); }
  .item.on .pip { background: #fff; }
  footer { display: flex; align-items: center; justify-content: space-between; gap: 6px; padding: 8px 12px 10px; border-top: 1px solid var(--border); flex-shrink: 0; }
  .rail footer { flex-direction: column; padding: 8px 0 10px; }
  .st { display: flex; align-items: center; gap: 7px; min-width: 0; font-size: 11.5px; color: var(--text-2); }
  .m { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  @media (max-width: 1000px) { .side:not(.rail) { width: 164px; } }
  @media (prefers-reduced-motion: reduce) { * { transition: none !important; } }
</style>
