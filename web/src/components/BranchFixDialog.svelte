<script lang="ts">
  import { app, loadCommits, loadIssues, toast } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import type { BranchFix } from '../lib/types';

  // The same fix on the other branches that still have the bug. Nothing is checked out or pushed.
  let picked = $state<Record<string, boolean>>({});
  let busy = $state(false);
  let results = $state.raw<BranchFix[] | null>(null);
  let shownFor = '';
  $effect(() => {
    const d = app.branchFix;
    if (d && d.commit !== shownFor) {
      shownFor = d.commit;
      results = null;
      picked = Object.fromEntries(d.targets.filter((t) => t.status === 'ready').map((t) => [t.branch, true]));
    }
    if (!d) shownFor = '';
  });
  const list = $derived(results ?? app.branchFix?.targets ?? []);
  const ready = $derived(list.filter((t) => t.status === 'ready'));
  const blocked = $derived(list.filter((t) => t.status === 'conflict' || t.status === 'skipped'));
  const fixed = $derived(list.filter((t) => t.status === 'fixed'));
  const fine = $derived(list.filter((t) => t.status === 'unaffected' || t.status === 'already'));
  const chosen = $derived(ready.filter((t) => picked[t.branch]).map((t) => t.branch));

  async function apply() {
    const d = app.branchFix;
    if (!d || !chosen.length) return;
    busy = true;
    try {
      results = await api.applyToBranches(d.commit, chosen);
      const n = results.filter((r) => r.status === 'fixed').length;
      if (n) toast(`Fixed on ${n} more branch${n > 1 ? 'es' : ''}`, 'good', 3500);
      await Promise.all([loadIssues(), loadCommits()]);
    } catch (e) {
      toast((e as Error).message, 'bad');
    } finally {
      busy = false;
    }
  }
  function close() {
    if (!busy) app.branchFix = null;
  }
  function key(e: KeyboardEvent) {
    if (app.branchFix && e.key === 'Escape') close();
  }
</script>

<svelte:window onkeydown={key} />

{#if app.branchFix}
  <div class="backdrop"></div>
  <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="bf-title">
    <header>
      <h2 id="bf-title">{results ? 'Other branches' : ready.length ? `${ready.length === 1 ? 'One other branch has' : `${ready.length} other branches have`} this bug` : 'Other branches'}</h2>
      <p class="muted">{app.branchFix.title}</p>
    </header>
    <div class="body">
      {#if fixed.length}
        <div class="glabel">Fixed</div>
        <ul>{#each fixed as t (t.branch)}<li><span class="ok">✓</span><span class="mono name">{t.branch}</span><span class="faint mono">{t.commit?.slice(0, 7)}</span></li>{/each}</ul>
      {/if}
      {#if ready.length}
        <div class="glabel">Apply the fix</div>
        <ul>
          {#each ready as t (t.branch)}
            <li><label><input type="checkbox" bind:checked={picked[t.branch]} /><span class="mono name">{t.branch}</span></label></li>
          {/each}
        </ul>
      {/if}
      {#if blocked.length}
        <div class="glabel">Needs you</div>
        <ul>
          {#each blocked as t (t.branch)}
            <li class="col"><span class="mono name">{t.branch}</span><span class="faint">{t.detail}{#if t.files?.length}: {t.files.join(', ')}{/if}</span></li>
          {/each}
        </ul>
      {/if}
      {#if fine.length}
        <p class="faint fine">Nothing to do on {fine.map((t) => t.branch).join(', ')}.</p>
      {/if}
      {#if !results && chosen.length}<p class="faint fine">Each branch gets a new commit on top. Nothing is checked out or pushed.</p>{/if}
    </div>
    <footer>
      <button class="btn secondary" disabled={busy} onclick={close}>{results || !ready.length ? 'Done' : 'Not now'}</button>
      {#if ready.length && !results}
        <button class="btn" disabled={busy || !chosen.length} onclick={apply}>
          {#if busy}<span class="spinner"></span>{/if}{chosen.length === ready.length && ready.length > 1 ? `Fix all ${ready.length} branches` : chosen.length === 1 ? `Fix ${chosen[0]}` : `Fix ${chosen.length} branches`}
        </button>
      {/if}
    </footer>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 60; background: rgba(0, 0, 0, 0.4); }
  .dialog { position: fixed; z-index: 61; top: 50%; left: 50%; transform: translate(-50%, -50%); width: min(480px, calc(100vw - 32px)); max-height: calc(100vh - 64px); display: flex; flex-direction: column; border-radius: 13px; background: var(--surface); box-shadow: var(--shadow); }
  header { padding: 18px 20px 6px; }
  h2 { margin: 0; font-size: 15px; font-weight: 600; }
  header p { margin: 3px 0 0; }
  .body { padding: 8px 20px 12px; overflow: auto; min-height: 0; }
  .glabel { margin: 10px 0 5px; font-size: 12px; font-weight: 600; color: var(--text-2); }
  ul { list-style: none; margin: 0; padding: 2px 12px; border-radius: var(--r-lg); background: var(--surface-2); }
  li { display: flex; align-items: center; gap: 8px; min-height: 34px; border-bottom: 1px solid var(--border); }
  li:last-child { border-bottom: 0; }
  li.col { flex-direction: column; align-items: flex-start; gap: 1px; padding: 7px 0; font-size: 12px; }
  label { display: flex; align-items: center; gap: 9px; width: 100%; cursor: pointer; }
  .name { font-size: 12.5px; flex: 1; min-width: 0; overflow-wrap: anywhere; }
  .ok { color: var(--good); }
  .fine { margin: 10px 2px 0; font-size: 12px; }
  footer { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 20px 16px; }
</style>
