<script lang="ts">
  import { app, selectNode } from '../lib/store.svelte';
  import Glyph from './Glyph.svelte';
  import BranchPicker from './BranchPicker.svelte';
  import CopyButton from './CopyButton.svelte';

  const TITLES: Record<string, string> = { review: 'Review', repo: 'Repository', branches: 'Branches', mine: 'My work', team: 'Team', release: 'Release', incidents: 'Incidents', activity: 'Activity', memory: 'Memory' };
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

  {#if app.mode === 'review' || app.mode === 'incidents'}<h1 class="title">{TITLES[app.mode]}</h1>{/if}

  <span class="repo" title={app.status?.path}>{app.status?.repo ?? '…'}</span>
  {#if app.status}
    {#if app.featureOn('branchSwitcher')}<BranchPicker />{:else}<span class="branch mono">{app.status.tracking}</span>{/if}
    {#if app.status.head}<span class="sha mono">{app.status.head.slice(0, 7)}</span><CopyButton text={app.status.head} title="Copy the full commit id" />{/if}
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
        placeholder="Search for a class"
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
              <span class="rpkg mono">{n.module ? `${n.module} · ` : ''}{n.pkg}</span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>

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
    height: 48px;
    padding: 0 10px;
    border-bottom: 1px solid var(--border);
    background: var(--surface);
    flex-shrink: 0;
    white-space: nowrap;
  }
  .title { margin: 0 4px 0 2px; font-size: 15px; font-weight: 600; letter-spacing: -0.005em; }
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
    border: 0;
    border-radius: var(--r);
    background: var(--surface-3);
    color: var(--text-2);
  }
  .field.focused { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
  .field input { flex: 1; min-width: 0; border: 0; outline: none; background: none; color: var(--text); font: inherit; }
  .field input::placeholder { color: var(--text-2); }

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


  @media (max-width: 1000px) {
    .repo, .sha { display: none; }
  }
</style>
