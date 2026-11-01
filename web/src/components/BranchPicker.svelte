<script lang="ts">
  import { app, followBranch } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import { ago } from '../lib/theme';
  import type { BranchRef } from '../lib/types';

  // One click to look at any branch: nothing is checked out and no files change.
  let open = $state(false);
  let refs = $state.raw<BranchRef[]>([]);
  let query = $state('');
  let box = $state<HTMLDivElement>();
  async function toggle() {
    open = !open;
    if (open) {
      query = '';
      try { refs = await api.refs(); } catch { refs = []; }
    }
  }
  function pick(name: string) {
    open = false;
    if (name !== app.status?.branch) void followBranch(name);
  }
  const shown = $derived(refs.filter((r) => !query || `${r.name} ${r.by}`.toLowerCase().includes(query.toLowerCase())));
  function outside(e: MouseEvent) { if (open && box && !box.contains(e.target as Node)) open = false; }
</script>

<svelte:window onmousedown={outside} onkeydown={(e) => { if (e.key === 'Escape') open = false; }} />

<div class="wrap" bind:this={box}>
  <button class="cur mono" aria-expanded={open} title="Switch branch" onclick={toggle}>
    {app.status?.tracking}
    <svg width="10" height="10" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2"><path d="m4 6 4 4 4-4"/></svg>
  </button>
  {#if app.status?.viewing}
    <span class="ro" title="{app.status.checkedOut} is checked out, so fixes and commits are off here">read-only</span>
    <button class="back mono" title="Go back to the checked-out branch" onclick={() => pick(app.status!.checkedOut!)}>back to {app.status.checkedOut}</button>
  {/if}
  {#if open}
    <div class="menu" role="menu">
      <input bind:value={query} placeholder="Find a branch…" spellcheck="false" />
      <ul>
        {#each shown as r (r.name)}
          <li>
            <button class:on={r.name === app.status?.branch} onclick={() => pick(r.name)}>
              <span class="n mono">{r.name}{#if r.checkedOut} <em>checked out</em>{/if}</span>
              <span class="m">{r.message}</span>
              <span class="w">{r.by} · {ago(r.at)}</span>
            </button>
          </li>
        {:else}<li class="none">No branches.</li>{/each}
      </ul>
    </div>
  {/if}
</div>

<style>
  .wrap { position: relative; display: inline-flex; align-items: center; gap: 6px; }
  .cur { display: inline-flex; align-items: center; gap: 5px; height: 24px; padding: 0 7px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); color: var(--accent-text); font-size: 12px; white-space: nowrap; }
  .cur:hover { border-color: var(--border-strong); }
  .ro { font-size: 10.5px; padding: 0 6px; line-height: 17px; border-radius: 9px; white-space: nowrap; color: var(--warn); background: color-mix(in srgb, var(--warn) 15%, transparent); }
  .back { border: 0; background: none; padding: 0; font-size: 11px; color: var(--accent-text); white-space: nowrap; }
  .back:hover { text-decoration: underline; }
  .menu { position: absolute; top: 30px; left: 0; z-index: 40; width: 380px; padding: 6px; border: 1px solid var(--border-strong); border-radius: var(--r-lg); background: var(--surface); box-shadow: var(--shadow); }
  input { width: 100%; height: 28px; padding: 0 8px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12px; }
  ul { list-style: none; margin: 5px 0 0; padding: 0; max-height: 360px; overflow: auto; }
  li button { display: grid; gap: 1px; width: 100%; padding: 6px 8px; border: 0; border-radius: var(--r); background: none; text-align: left; }
  li button:hover { background: var(--hover); }
  li button.on { background: var(--accent-soft); }
  .n { font-size: 12px; color: var(--text); }
  em { font: 500 10px var(--font); font-style: normal; color: var(--good); margin-left: 4px; }
  .m { font-size: 11.5px; color: var(--text-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .w { font-size: 10.5px; color: var(--text-3); }
  .none { padding: 8px; color: var(--text-3); }
</style>
