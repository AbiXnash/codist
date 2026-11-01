<script lang="ts">
  import { onMount } from 'svelte';
  import { loadCommits, loadIssues, loadWorking, openFile, app, toast } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import CopyButton from './CopyButton.svelte';
  import type { PlannedCommit } from '../lib/types';

  // The local changes split into the commits they should become, each with a message you can edit.
  let plan = $state<PlannedCommit[] | null>(null);
  let error = $state('');
  let busy = $state<number | null>(null);
  let aiBusy = $state<number | null>(null);
  const load = async () => {
    try { plan = await api.plan(); error = ''; } catch (e) { error = (e as Error).message; }
  };
  onMount(() => void load());
  // Reload the plan when the set of changed files changes.
  let seen = '';
  $effect(() => {
    const k = (app.working?.changes ?? []).map((c) => c.path).join('|');
    if (seen && k !== seen) void load();
    seen = k;
  });
  const name = (p: string) => p.slice(p.lastIndexOf('/') + 1);

  async function better(i: number) {
    aiBusy = i;
    try {
      const m = await api.planMessage(plan![i].files.map((f) => f.path));
      plan![i].message = m.message;
      plan![i].why = m.why || plan![i].why;
    } catch (e) {
      if ((e as Error).name !== 'AbortError') toast((e as Error).message, 'bad');
    } finally { aiBusy = null; }
  }
  async function commit(i: number) {
    busy = i;
    try {
      const r = await api.commitPaths(plan![i].files.map((f) => f.path), plan![i].message);
      toast(`Committed ${r.commit.slice(0, 7)}`, 'good', 2500);
      await Promise.all([loadCommits(), loadWorking(), loadIssues()]);
      await load();
    } catch (e) {
      toast((e as Error).message, 'bad');
    } finally { busy = null; }
  }
</script>

<div class="plan">
  {#if error}<p class="faint">{error}</p>
  {:else if !plan}<span class="spinner"></span>
  {:else if plan.length}
    <p class="head"><b>{plan.length === 1 ? 'One commit' : `${plan.length} commits`}</b> recommended</p>
    {#each plan as g, i}
      <section>
        <div class="row"><textarea class="mono" rows="2" bind:value={g.message} spellcheck="false" aria-label="Commit message"></textarea><CopyButton text={g.message} title="Copy the message" /></div>
        <p class="why">{g.why}</p>
        <ul>
          {#each g.files as f}
            <li><button class="lnk mono" title={f.path} onclick={() => openFile(f.path, null)}>{name(f.path)}</button><span class="n"><i class="a">+{f.added}</i> <i class="d">−{f.removed}</i>{#if f.issues} · {f.issues} issue{f.issues > 1 ? 's' : ''}{/if}</span></li>
          {/each}
        </ul>
        <div class="row">
          <button class="btn secondary sm" disabled={aiBusy !== null} onclick={() => better(i)}>{#if aiBusy === i}<span class="spinner"></span>{/if}AI message</button>
          <button class="btn sm" disabled={busy !== null || !g.message.trim()} title="Commits only these files" onclick={() => commit(i)}>{#if busy === i}<span class="spinner"></span>{/if}Commit {g.files.length} file{g.files.length > 1 ? 's' : ''}</button>
        </div>
      </section>
    {/each}
  {/if}
</div>

<style>
  .plan { display: grid; gap: 8px; margin: 2px 0 8px; font-size: 12px; }
  .head { margin: 0; color: var(--text-2); }
  section { display: grid; gap: 5px; padding: 8px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  textarea { flex: 1; min-width: 0; padding: 5px 7px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface); color: var(--text); font-size: 11.5px; resize: vertical; }
  .why { margin: 0; font-size: 11px; color: var(--text-3); }
  ul { list-style: none; margin: 0; padding: 0; display: grid; gap: 1px; }
  li { display: flex; justify-content: space-between; gap: 6px; font-size: 11px; }
  .lnk { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11px; }
  .n { white-space: nowrap; color: var(--text-3); }
  i { font-style: normal; } .a { color: var(--added); } .d { color: var(--bad); }
  .row { display: flex; align-items: flex-start; gap: 5px; }
</style>
