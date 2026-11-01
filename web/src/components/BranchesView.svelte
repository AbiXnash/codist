<script lang="ts">
  import { onMount } from 'svelte';
  import { app, followBranch } from '../lib/store.svelte';
  import CopyButton from './CopyButton.svelte';
  import { api } from '../lib/api';
  import { ago } from '../lib/theme';
  import type { BranchReport } from '../lib/types';

  let r = $state.raw<BranchReport | null>(null);
  let error = $state('');
  let busy = $state(false);
  let showMerged = $state(false);
  let query = $state('');
  async function load() {
    busy = true;
    try { r = await api.branches(); error = ''; } catch (e) { error = (e as Error).message; } finally { busy = false; }
  }
  onMount(() => void load());
  const name = (p: string) => p.slice(p.lastIndexOf('/') + 1);
  async function view(n: string) {
    await followBranch(n);
    app.mode = 'review';
  }
  const who = (n: string) => (app.isMe(n) ? 'you' : n);
  const list = $derived((r?.branches ?? []).filter((b) => (showMerged || !b.merged) && (!query || `${b.name} ${b.lastBy} ${b.authors.join(' ')}`.toLowerCase().includes(query.toLowerCase()))));
  const conflicts = $derived(r?.alerts.filter((a) => a.severity === 'conflict').length ?? 0);
  const overlaps = $derived(r?.alerts.filter((a) => a.severity === 'overlap').length ?? 0);
</script>

<div class="page"><div class="page-in">
  <header class="h">
    <div><h1>Branches</h1><p>Every branch against {r?.base ?? 'the tracked branch'} and against each other: who collides with whom, before anyone merges. Nothing is checked out or changed.</p></div>
    <button class="btn secondary sm" onclick={load} disabled={busy}>{#if busy}<span class="spinner"></span>{/if}Refresh</button>
  </header>
  {#if error}<div class="pcard">{error}</div>
  {:else if !r}<div class="empty"><span class="spinner"></span></div>
  {:else}
    <div class="cards">
      <div class="stat"><b>{r.branches.filter((b) => !b.merged).length}</b><span>active branches</span></div>
      <div class="stat"><b class:bad={conflicts > 0}>{conflicts}</b><span>conflicts ahead</span></div>
      <div class="stat"><b class:warn={overlaps > 0}>{overlaps}</b><span>overlaps</span></div>
      <div class="stat"><b>{r.local.length}</b><span>files changed locally</span></div>
      <div class="stat"><b>{r.tookMs} ms</b><span>to compare them all</span></div>
    </div>

    <section class="pcard">
      <h2>Alerts</h2>
      {#each r.alerts as a}
        <div class="alert {a.severity}">
          <div class="t"><span class="tag">{a.severity}</span><b>{a.title}</b><CopyButton title="Copy this alert" text={`${a.title}\n${a.detail}\n${a.files.join('\n')}`} /></div>
          <p>{a.detail}</p>
          {#if a.people.length}<p class="small">With: <b>{a.people.map(who).join(', ')}</b></p>{/if}
          <p class="small mono files" title={a.files.join('\n')}>{a.files.slice(0, 6).map(name).join(', ')}{a.files.length > 6 ? ` and ${a.files.length - 6} more` : ''}</p>
        </div>
      {:else}<p class="good">No conflicts or overlaps between branches.</p>{/each}
    </section>

    <section class="pcard">
      <div class="row"><h2>All branches</h2>
        <input bind:value={query} placeholder="Filter by branch or person…" spellcheck="false" />
        <label class="small"><input type="checkbox" bind:checked={showMerged} /> Show merged</label>
      </div>
      <div style="overflow-x:auto"><table class="tbl">
        <thead><tr><th>Branch</th><th>People</th><th class="num">Ahead</th><th class="num">Behind</th><th class="num">Files</th><th>Merge into {r.base}</th><th>Last commit</th></tr></thead>
        <tbody>
          {#each list as b}
            <tr>
              <td class="mono small"><button class="lnk" title="Look at this branch" onclick={() => view(b.name)}>{b.name}</button><CopyButton text={b.name} title="Copy the branch name" />{#if b.remote} <span class="faint">remote</span>{/if}</td>
              <td class="small">{(b.authors.length ? b.authors : [b.lastBy]).map(who).join(', ')}</td>
              <td class="num">{b.ahead}</td><td class="num" class:warn={b.behind > 20}>{b.behind}</td><td class="num">{b.files.length}</td>
              <td class="small">{#if b.merged}<span class="faint">nothing to merge</span>{:else if b.conflicts.length}<span class="bad" title={b.conflicts.join('\n')}>conflicts in {b.conflicts.length}</span>{:else}<span class="ok">clean</span>{/if}</td>
              <td class="small msg" title={b.lastMsg}>{b.lastMsg} <span class="faint">· {ago(b.lastAt)}</span></td>
            </tr>
          {:else}<tr><td colspan="7" class="faint">No other branches. Branches appear here as soon as they exist locally or on the remote.</td></tr>{/each}
        </tbody>
      </table></div>
    </section>
  {/if}
</div></div>

<style>
  .h { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; }
  .h h1 { margin: 0 0 3px; font-size: 20px; font-weight: 600; }
  .h p { margin: 0; color: var(--text-2); }
  .bad { color: var(--bad); } .warn { color: var(--warn); } .ok { color: var(--good); } .good { margin: 0; color: var(--good); }
  .small { font-size: 11.5px; }
  .alert { padding: 9px 11px; margin-bottom: 8px; border: 1px solid var(--border); border-left: 3px solid var(--warn); border-radius: var(--r); background: var(--surface-2); }
  .alert.conflict { border-left-color: var(--bad); }
  .alert .t { display: flex; align-items: center; gap: 8px; }
  .alert p { margin: 3px 0 0; color: var(--text-2); }
  .alert .files { color: var(--accent-text); }
  .tag { font: 600 10px var(--font); padding: 0 6px; line-height: 16px; border-radius: 8px; color: var(--warn); background: color-mix(in srgb, var(--warn) 14%, transparent); }
  .conflict .tag { color: var(--bad); background: color-mix(in srgb, var(--bad) 14%, transparent); }
  .row { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
  .row h2 { margin: 0; }
  .row input:not([type]) { flex: 1; height: 26px; padding: 0 8px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12px; }
  .msg { max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
