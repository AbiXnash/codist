<script lang="ts">
  import { app, selectNode, setTheme } from '../lib/store.svelte';
  import Glyph from './Glyph.svelte';

  let input: HTMLInputElement;
  let focused = $state(false);
  let active = $state(0);

  const matches = $derived.by(() => {
    const q = app.search.trim().toLowerCase();
    if (!q || !app.graph) return [];
    return app.graph.nodes
      .filter((n) => n.name.toLowerCase().includes(q))
      .sort((a, b) => Number(!a.name.toLowerCase().startsWith(q)) - Number(!b.name.toLowerCase().startsWith(q)) || a.name.length - b.name.length)
      .slice(0, 8);
  });

  function pick(id: string) {
    selectNode(id);
    app.showPanel = true;
    app.search = '';
    input.blur();
    window.dispatchEvent(new CustomEvent('codist:center', { detail: id }));
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') { active = Math.min(active + 1, matches.length - 1); e.preventDefault(); }
    if (e.key === 'ArrowUp') { active = Math.max(active - 1, 0); e.preventDefault(); }
    if (e.key === 'Enter' && matches[active]) pick(matches[active].id);
    if (e.key === 'Escape') { app.search = ''; input.blur(); }
  }

  function globalKey(e: KeyboardEvent) {
    if ((e.key === '/' || (e.key === 'k' && (e.metaKey || e.ctrlKey))) && document.activeElement?.tagName !== 'INPUT') {
      e.preventDefault();
      input.focus();
    }
  }
</script>

<svelte:window onkeydown={globalKey} />

<header class="bar">
  <button class="icon-btn" class:on={app.showCommits} title="Toggle commits  [" aria-label="Toggle commits" onclick={() => (app.showCommits = !app.showCommits)}>
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4"><rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M5.5 2.5v11"/>{#if app.showCommits}<path d="M2.5 5h2M2.5 7h2" stroke-linecap="round"/>{/if}</svg>
  </button>

  <div class="brand">
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="var(--accent)" stroke-width="2"><circle cx="6" cy="7" r="2.5"/><circle cx="18" cy="9" r="2.5"/><circle cx="10" cy="18" r="2.5"/><path d="M8.4 7.4 15.6 8.6M16.6 11.2l-4.9 5M7 9.3l2.2 6.3" stroke-opacity=".6"/></svg>
    Codist
  </div>

  <nav class="modes" aria-label="Mode">
    <button class:on={app.mode === 'review'} onclick={() => (app.mode = 'review')}>Review</button>
    <button class:on={app.mode === 'incidents'} onclick={() => (app.mode = 'incidents')}>Incidents{#if app.incidents.some((i) => i.status === 'analysing')}<span class="spinner tiny"></span>{/if}</button>
    <button class:on={app.mode === 'release'} onclick={() => (app.mode = 'release')}>Release</button>
  </nav>

  <span class="sep">/</span>
  <span class="repo" title={app.status?.path}>{app.status?.repo ?? '…'}</span>
  {#if app.status}
    <span class="branch mono">{app.status.tracking}</span>
    {#if app.status.head}<span class="sha mono">{app.status.head.slice(0, 7)}</span>{/if}
  {/if}

  <div class="search">
    <div class="field" class:focused>
      <svg width="13" height="13" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="2"><circle cx="9" cy="9" r="6"/><path d="m14 14 4 4" stroke-linecap="round"/></svg>
      <input
        bind:this={input}
        bind:value={app.search}
        onfocus={() => (focused = true)}
        onblur={() => setTimeout(() => (focused = false), 120)}
        oninput={() => (active = 0)}
        onkeydown={onKey}
        placeholder="Go to class…"
        spellcheck="false"
        autocomplete="off"
      />
      <kbd>/</kbd>
    </div>
    {#if focused && matches.length}
      <ul class="results">
        {#each matches as n, i (n.id)}
          <li>
            <button class:active={i === active} onmousedown={() => pick(n.id)}>
              <Glyph stereotype={n.stereotype} />
              <span class="rname">{n.name}</span>
              <span class="rpkg mono">{n.pkg}</span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>

  <span class="status" title={app.status?.llmProblem || `Ollama ready · review ${app.status?.models?.review} · summary ${app.status?.models?.summary} · fix ${app.status?.models?.fix} · ask ${app.status?.models?.ask} · ${app.status?.rules} rules`}>
    <span class="dot" style:background={app.status?.llmProblem ? 'var(--bad)' : 'var(--good)'}></span>
    <span class="mono">{app.status?.model ?? 'model'}</span>
  </span>
  <span class="status" title={app.live ? 'Receiving live updates' : 'Disconnected, retrying'}>
    <span class="dot" style:background={app.live ? 'var(--good)' : 'var(--bad)'}></span>
    {app.live ? 'Live' : 'Offline'}
  </span>

  <button class="icon-btn" title="Switch to {app.theme === 'dark' ? 'light' : 'dark'} theme" aria-label="Toggle theme" onclick={() => setTheme(app.theme === 'dark' ? 'light' : 'dark')}>
    {#if app.theme === 'dark'}
      <svg width="15" height="15" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><circle cx="10" cy="10" r="3.5"/><path d="M10 1.5v2M10 16.5v2M1.5 10h2M16.5 10h2M4 4l1.4 1.4M14.6 14.6 16 16M4 16l1.4-1.4M14.6 5.4 16 4"/></svg>
    {:else}
      <svg width="15" height="15" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M16.5 12.5A7 7 0 0 1 7.5 3.5a7 7 0 1 0 9 9Z"/></svg>
    {/if}
  </button>
  <button class="icon-btn" class:on={app.showPanel} title="Toggle details panel  ]" aria-label="Toggle details panel" onclick={() => (app.showPanel = !app.showPanel)}>
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4"><rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M9.5 2.5v11"/></svg>
  </button>
</header>

<style>
  .bar {
    position: relative;
    z-index: 20;
    display: flex;
    align-items: center;
    gap: 10px;
    height: 44px;
    padding: 0 8px;
    border-bottom: 1px solid var(--border);
    background: var(--surface);
    flex-shrink: 0;
    white-space: nowrap;
  }
  .modes { display: flex; gap: 2px; padding: 2px; margin-left: 4px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .modes button { display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 10px; border: 0; border-radius: var(--r); background: none; color: var(--text-3); font-size: 12.5px; font-weight: 500; }
  .modes button:hover { color: var(--text); }
  .modes button.on { color: var(--text); background: var(--surface-3); box-shadow: 0 0 0 1px var(--border-strong); }
  .tiny { width: 9px; height: 9px; }
  .brand { display: flex; align-items: center; gap: 7px; font-weight: 600; letter-spacing: -0.01em; }
  .sep { color: var(--text-3); }
  .repo { font-weight: 500; overflow: hidden; text-overflow: ellipsis; }
  .branch { color: var(--accent-text); }
  .sha { color: var(--text-3); }

  .search { position: relative; flex: 1; display: flex; justify-content: center; padding: 0 12px; }
  .field {
    display: flex;
    align-items: center;
    gap: 8px;
    width: min(380px, 100%);
    height: 28px;
    padding: 0 6px 0 10px;
    border: 1px solid var(--border);
    border-radius: var(--r);
    background: var(--surface-2);
    color: var(--text-3);
  }
  .field.focused { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
  .field input { flex: 1; min-width: 0; border: 0; outline: none; background: none; color: var(--text); font: inherit; }
  .field input::placeholder { color: var(--text-3); }

  .results {
    position: absolute;
    top: 34px;
    width: min(380px, 100%);
    margin: 0;
    padding: 4px;
    list-style: none;
    border: 1px solid var(--border-strong);
    border-radius: var(--r-lg);
    background: var(--surface);
    box-shadow: var(--shadow);
  }
  .results button { display: flex; align-items: center; gap: 8px; width: 100%; padding: 6px 8px; border: 0; border-radius: var(--r); background: none; text-align: left; }
  .results button.active, .results button:hover { background: var(--active); }
  .rname { font-weight: 500; }
  .rpkg { margin-left: auto; font-size: 11px; color: var(--text-3); overflow: hidden; text-overflow: ellipsis; }

  .status { display: inline-flex; align-items: center; gap: 6px; padding: 0 6px; font-size: 12px; color: var(--text-2); }
  .status .dot { width: 7px; height: 7px; }

  @media (max-width: 1000px) {
    .sep, .repo, .branch, .sha, .status { display: none; }
  }
</style>
