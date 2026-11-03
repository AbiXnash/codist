<script lang="ts">
  import { onMount } from 'svelte';
  import { app, boot, savePref, saveStr, selectCommit } from './lib/store.svelte';
  import { preload } from './lib/highlight';
  import TopBar from './components/TopBar.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import LeftDock from './components/LeftDock.svelte';
  import BranchGraph from './components/BranchGraph.svelte';
  import RepoView from './components/RepoView.svelte';
  import BranchesView from './components/BranchesView.svelte';
  import PeopleView from './components/PeopleView.svelte';
  import ActivityView from './components/ActivityView.svelte';
  import MemoryView from './components/MemoryView.svelte';
  import DiffReview from './components/DiffReview.svelte';
  import Panel from './components/Panel.svelte';
  import Toasts from './components/Toasts.svelte';
  import FixDialog from './components/FixDialog.svelte';
  import IncidentsView from './components/IncidentsView.svelte';
  import ReleaseView from './components/ReleaseView.svelte';

  let dragging = $state<'side' | 'panel' | null>(null);

  // The screen is in the address, so a reload or a bookmark lands in the same place.
  const SCREENS = ['review', 'graph', 'incidents', 'memory', 'activity', 'repo', 'branches', 'mine', 'team', 'release'];
  const wanted = location.hash.slice(1);
  if (SCREENS.includes(wanted)) app.mode = wanted as typeof app.mode;
  $effect(() => {
    history.replaceState(null, '', `#${app.mode}`);
  });

  onMount(() => {
    for (const w of Object.values(WIDTHS)) {
      let saved: string | null = null;
      try {
        saved = localStorage.getItem('codist.' + w.pref);
      } catch {
        /* storage is blocked: use the default width */
      }
      if (saved && Number(saved) >= w.min) document.documentElement.style.setProperty(w.name, `${saved}px`);
    }
    preload();
    void boot();
  });

  // Persist layout choices and let the graph canvas re-measure after the grid changes.
  $effect(() => {
    savePref('commits', app.showCommits);
    savePref('panel', app.showPanel);
    savePref('legend', app.showLegend);
    requestAnimationFrame(() => window.dispatchEvent(new Event('codist:resize')));
  });

  // Both side panels can be dragged or moved with the arrow keys, and remember their width.
  const WIDTHS = {
    side: { name: '--side-w', pref: 'sideWidth', min: 200, other: '--panel-w' },
    panel: { name: '--panel-w', pref: 'panelWidth', min: 360, other: '--side-w' },
  } as const;
  const CENTRE_MIN = 360;
  type Edge = keyof typeof WIDTHS;

  function setWidth(edge: Edge, wanted: number) {
    const w = WIDTHS[edge];
    const shownOther = edge === 'side' ? app.showPanel : app.showCommits;
    const other = shownOther ? (document.querySelector(edge === 'side' ? '.panel' : '.dock') as HTMLElement | null)?.offsetWidth ?? 0 : 0;
    const left = (document.querySelector('.side') as HTMLElement | null)?.offsetWidth ?? 0;
    const max = Math.max(w.min, window.innerWidth - left - other - CENTRE_MIN);
    const width = Math.round(Math.min(Math.max(wanted, w.min), max));
    document.documentElement.style.setProperty(w.name, `${width}px`);
    saveStr(w.pref, String(width));
    window.dispatchEvent(new Event('codist:resize'));
  }
  function startResize(edge: Edge, e: PointerEvent) {
    dragging = edge;
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
  }
  function resize(e: PointerEvent) {
    if (!dragging) return;
    if (dragging === 'panel') setWidth('panel', window.innerWidth - e.clientX);
    else setWidth('side', e.clientX - ((document.querySelector('.dock') as HTMLElement | null)?.getBoundingClientRect().left ?? 0));
  }
  function resizeKey(edge: Edge, e: KeyboardEvent) {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    e.preventDefault();
    const box = document.querySelector(edge === 'side' ? '.dock' : '.panel') as HTMLElement | null;
    const grow = (e.key === 'ArrowRight') === (edge === 'side');
    setWidth(edge, (box?.offsetWidth ?? 0) + (grow ? 24 : -24));
  }

  function keys(e: KeyboardEvent) {
    if ((e.target as Element | null)?.closest?.('input, textarea, select') || e.metaKey || e.ctrlKey || e.altKey || app.fix || app.mode !== 'review') return;
    if (e.key === '\\') app.showSidebar = !app.showSidebar;
    else if (e.key === '[') app.showCommits = !app.showCommits;
    else if (e.key === ']') app.showPanel = !app.showPanel;
    else if (e.key === 'J' || e.key === 'K' || (app.mode !== 'review' && (e.key === 'j' || e.key === 'k'))) {
      const list = app.shownCommits;
      const i = list.findIndex((c) => c.commit.sha === app.selectedSha);
      const next = list[Math.max(0, Math.min(list.length - 1, i + (e.key.toLowerCase() === 'j' ? 1 : -1)))];
      if (next) void selectCommit(next.commit.sha, true);
    } else if ('12345'.includes(e.key) && e.key.length === 1) {
      const panels = ['issues', 'commit', 'commit', 'code', 'ask'];
      if (app.panelOn(panels[Number(e.key) - 1])) {
        app.tab = (['issues', 'review', 'diff', 'code', 'ask'] as const)[Number(e.key) - 1];
        app.showPanel = true;
      }
    }
  }
</script>

<svelte:window onkeydown={keys} />

<div class="window">
<Sidebar />
<div class="shell">
  <TopBar />
  {#if app.status?.operation}
    <div class="op" role="status">
      <b>{app.status.operation[0].toUpperCase() + app.status.operation.slice(1)} in progress</b>
      {#if app.status.conflicts?.length}· {app.status.conflicts.length} file{app.status.conflicts.length === 1 ? '' : 's'} with conflicts: <span class="mono">{app.status.conflicts.map((p) => p.slice(p.lastIndexOf('/') + 1)).join(', ')}</span>{/if}
      · fixes are paused until it is finished or aborted.
    </div>
  {/if}
  {#if app.mode === 'incidents'}
    <IncidentsView />
  {:else if ['repo', 'branches', 'mine', 'team', 'release'].includes(app.mode)}
    <!-- These describe the followed branch, so they load again when it changes. -->
    {#key app.status?.branch}
      {#if app.mode === 'release'}<ReleaseView />
      {:else if app.mode === 'repo'}<RepoView />
      {:else if app.mode === 'branches'}<BranchesView />
      {:else if app.mode === 'mine'}<PeopleView mode="mine" />
      {:else}<PeopleView mode="team" />{/if}
    {/key}
  {:else if app.mode === 'activity'}
    <ActivityView />
  {:else if app.mode === 'memory'}
    <MemoryView />
  {:else}
  <main
    class="layout"
    class:dragging={dragging !== null}
    class:no-side={!app.showCommits}
    class:no-panel={!app.showPanel}
  >
    {#if app.showCommits}
      <LeftDock />
      <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
      <div class="resizer" role="separator" aria-orientation="vertical" aria-label="Resize the commits panel" tabindex="0" onpointerdown={(e) => startResize('side', e)} onpointermove={resize} onpointerup={() => (dragging = null)} onkeydown={(e) => resizeKey('side', e)}></div>
    {/if}
    {#if app.mode === 'review'}
      <DiffReview />
    {:else if app.centre === 'branches'}
      <BranchGraph />
    {:else}
      <!-- The graph library is large, so it loads only when this screen opens. -->
      {#await import('./components/GraphView.svelte')}
        <div class="empty">Loading the graph</div>
      {:then { default: GraphView }}
        <GraphView />
      {/await}
    {/if}
    {#if app.showPanel}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
      <div class="resizer" role="separator" aria-orientation="vertical" aria-label="Resize the details panel" tabindex="0" onpointerdown={(e) => startResize('panel', e)} onpointermove={resize} onpointerup={() => (dragging = null)} onkeydown={(e) => resizeKey('panel', e)}></div>
      <Panel />
    {/if}
  </main>
  {/if}
</div>
</div>

<FixDialog />
<Toasts />

<style>
  .window { height: 100%; display: flex; }
  .shell { flex: 1; min-width: 0; height: 100%; display: flex; flex-direction: column; }
  .op { padding: 7px 16px; font-size: 12.5px; color: var(--text); background: color-mix(in srgb, var(--warn) 14%, var(--surface)); border-bottom: 1px solid color-mix(in srgb, var(--warn) 40%, transparent); }
  .op .mono { font-size: 11.5px; }
  .layout {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: var(--side-w) 1px minmax(0, 1fr) 1px var(--panel-w);
  }
  .layout.no-side { grid-template-columns: minmax(0, 1fr) 1px var(--panel-w); }
  .layout.no-panel { grid-template-columns: var(--side-w) 1px minmax(0, 1fr); }
  .layout.no-side.no-panel { grid-template-columns: minmax(0, 1fr); }
  .layout.dragging { cursor: col-resize; user-select: none; }

  .resizer { position: relative; background: var(--border); cursor: col-resize; touch-action: none; z-index: 5; }
  .resizer::after { content: ''; position: absolute; inset: 0 -4px; }
  .resizer:hover, .resizer:focus-visible, .dragging .resizer { background: var(--accent); }

  @media (max-width: 1000px) {
    :global(body) { overflow: auto; }
    .window { height: auto; min-height: 100%; }
    .shell { height: auto; min-height: 100%; }
    .layout, .layout.no-side, .layout.no-panel, .layout.no-side.no-panel { grid-template-columns: 1fr; }
    .resizer { display: none; }
  }
</style>
