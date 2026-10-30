<script lang="ts">
  import { onMount } from 'svelte';
  import { app, boot, savePref, selectCommit } from './lib/store.svelte';
  import { preload } from './lib/highlight';
  import TopBar from './components/TopBar.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import GraphView from './components/GraphView.svelte';
  import Panel from './components/Panel.svelte';
  import Toasts from './components/Toasts.svelte';
  import FixDialog from './components/FixDialog.svelte';
  import IncidentsView from './components/IncidentsView.svelte';
  import ReleaseView from './components/ReleaseView.svelte';

  let dragging = $state(false);

  onMount(() => {
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

  function startResize(e: PointerEvent) {
    dragging = true;
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
  }
  function resize(e: PointerEvent) {
    if (!dragging) return;
    const w = Math.min(Math.max(window.innerWidth - e.clientX, 360), window.innerWidth - 560);
    document.documentElement.style.setProperty('--panel-w', `${w}px`);
    window.dispatchEvent(new Event('codist:resize'));
  }

  function keys(e: KeyboardEvent) {
    if ((e.target as Element | null)?.closest?.('input, textarea, select') || e.metaKey || e.ctrlKey || e.altKey || app.fix || app.mode !== 'review') return;
    if (e.key === '[') app.showCommits = !app.showCommits;
    else if (e.key === ']') app.showPanel = !app.showPanel;
    else if (e.key === 'j' || e.key === 'k') {
      const i = app.commits.findIndex((c) => c.commit.sha === app.selectedSha);
      const next = app.commits[Math.max(0, Math.min(app.commits.length - 1, i + (e.key === 'j' ? 1 : -1)))];
      if (next) void selectCommit(next.commit.sha);
    } else if ('12345'.includes(e.key) && e.key.length === 1) {
      app.tab = (['review', 'code', 'diff', 'type', 'ask'] as const)[Number(e.key) - 1];
      app.showPanel = true;
    }
  }
</script>

<svelte:window onkeydown={keys} />

<div class="shell">
  <TopBar />
  {#if app.mode === 'incidents'}
    <IncidentsView />
  {:else if app.mode === 'release'}
    <ReleaseView />
  {:else}
  <main
    class="layout"
    class:dragging
    class:no-side={!app.showCommits}
    class:no-panel={!app.showPanel}
  >
    {#if app.showCommits}<Sidebar />{/if}
    <GraphView />
    {#if app.showPanel}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="resizer" onpointerdown={startResize} onpointermove={resize} onpointerup={() => (dragging = false)}></div>
      <Panel />
    {/if}
  </main>
  {/if}
</div>

<FixDialog />
<Toasts />

<style>
  .shell { height: 100%; display: flex; flex-direction: column; }
  .layout {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: var(--side-w) minmax(0, 1fr) 1px var(--panel-w);
  }
  .layout.no-side { grid-template-columns: minmax(0, 1fr) 1px var(--panel-w); }
  .layout.no-panel { grid-template-columns: var(--side-w) minmax(0, 1fr); }
  .layout.no-side.no-panel { grid-template-columns: minmax(0, 1fr); }
  .layout.dragging { cursor: col-resize; user-select: none; }

  .resizer { position: relative; background: var(--border); cursor: col-resize; touch-action: none; z-index: 5; }
  .resizer::after { content: ''; position: absolute; inset: 0 -4px; }
  .resizer:hover, .dragging .resizer { background: var(--accent); }

  @media (max-width: 1000px) {
    :global(body) { overflow: auto; }
    .shell { height: auto; min-height: 100%; }
    .layout, .layout.no-side, .layout.no-panel, .layout.no-side.no-panel { grid-template-columns: 1fr; }
    .resizer { display: none; }
  }
</style>
