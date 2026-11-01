<script lang="ts">
  import { onMount } from 'svelte';
  import { toast, unignore } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import CopyButton from './CopyButton.svelte';
  import { ago } from '../lib/theme';
  import type { MemoryReport } from '../lib/types';

  // What has been added on top of the base model for this repository.
  let m = $state.raw<MemoryReport | null>(null);
  let text = $state('');
  let team = $state(false);
  async function load() {
    try { m = await api.memory(); } catch (e) { toast((e as Error).message, 'bad'); }
  }
  onMount(() => void load());
  async function add() {
    if (!text.trim()) return;
    try {
      await api.addMemory({ text, team, kind: 'preference' });
      text = '';
      toast(team ? 'Saved for the team in .codist/memory.yaml; commit that file to share it' : 'Saved. Later reviews and fixes will follow it', 'good', 4000);
      await load();
    } catch (e) { toast((e as Error).message, 'bad'); }
  }
  async function remove(id: string) { await api.removeMemory(id); await load(); }
  async function share(id: string, to: boolean) {
    await api.shareMemory(id, to);
    toast(to ? 'Moved to .codist/memory.yaml; commit that file to share it' : 'Now personal', 'good', 3500);
    await load();
  }
  async function dropIgnore(id: string) { await unignore(id); await load(); }
</script>

<div class="page"><div class="page-in">
  <header>
    <h1>Memory</h1>
    <p>What this repository has taught the model on top of its base knowledge. Items are added to the prompts of later reviews and fixes; the model itself is not retrained.</p>
  </header>
  {#if !m}<div class="empty"><span class="spinner"></span></div>
  {:else}
    <div class="cards">
      <div class="stat"><b>{m.items.length}</b><span>taught by people</span></div>
      <div class="stat"><b>{m.items.filter((i) => i.team).length}</b><span>shared with the team</span></div>
      <div class="stat"><b>{m.lessons.length}</b><span>learned from history</span></div>
      <div class="stat"><b>{m.ignores.length}</b><span>ignored</span></div>
      <div class="stat"><b>{m.rules.length}</b><span>custom rules</span></div>
      <div class="stat"><b>{m.builtinEntries + m.builtinRules}</b><span>built-in patterns</span></div>
    </div>

    <section class="pcard">
      <h2>Teach Codist</h2>
      <form onsubmit={(e) => { e.preventDefault(); void add(); }}>
        <textarea rows="2" bind:value={text} placeholder="e.g. We use constructor injection everywhere except legacy controllers. Never log card tokens."></textarea>
        <div class="row">
          <label class="small"><input type="checkbox" bind:checked={team} /> Share with the team (.codist/memory.yaml)</label>
          <button class="btn sm" disabled={!text.trim()}>Remember</button>
        </div>
      </form>
    </section>

    <section class="pcard">
      <h2>Taught by people</h2>
      {#each m.items as it (it.id)}
        <div class="it">
          <div class="body">
            <div>{it.text}</div>
            <div class="faint small">
              {it.kind === 'rejected-fix' ? 'Rejected fix' : 'Preference'}{#if it.title} · about “{it.title}”{/if} · {it.by || 'someone'} · {ago(it.at)}{#if it.team} · <span class="team">team</span>{/if}
            </div>
          </div>
          <CopyButton text={it.text} />
          <button class="btn secondary sm" onclick={() => share(it.id, !it.team)}>{it.team ? 'Make personal' : 'Share with team'}</button>
          <button class="btn secondary sm" onclick={() => remove(it.id)}>Remove</button>
        </div>
      {:else}<p class="faint">Nothing yet. Add something above, or reject a suggested fix and say why.</p>{/each}
    </section>

    <div class="split">
      <section class="pcard">
        <h2>Learned from history <span class="faint">fixes and dismissals, rebuilt automatically</span></h2>
        {#each m.lessons as l (l.id)}
          <details><summary>{l.title}</summary><pre>{l.body}</pre></details>
        {:else}<p class="faint">Fixed issues appear here after the next review.</p>{/each}
      </section>
      <section class="pcard">
        <h2>Ignored</h2>
        {#each m.ignores as ig (ig.id)}
          <div class="it">
            <div class="body"><div>{ig.title || ig.key}</div><div class="faint small">{ig.scope === 'rule' ? 'This rule everywhere' : ig.scope === 'code' ? 'This code' : 'One commit'}{#if ig.reason} · {ig.reason}{/if}{#if ig.team} · team{/if}</div></div>
            {#if !ig.team}<button class="btn secondary sm" onclick={() => dropIgnore(ig.id)}>Stop ignoring</button>{/if}
          </div>
        {:else}<p class="faint">Nothing ignored.</p>{/each}
        {#if m.rules.length}
          <h2 style="margin-top:14px">Custom rules</h2>
          {#each m.rules as r (r.id)}<div class="small"><b>{r.title}</b> <span class="faint mono">{r.source.slice(r.source.lastIndexOf('/') + 1)}</span></div>{/each}
        {/if}
      </section>
    </div>
  {/if}
</div></div>

<style>
  .small { font-size: 11.5px; }
  textarea { width: 100%; padding: 8px 10px; border: 1px solid var(--border-strong); border-radius: var(--r); background: var(--surface-2); color: var(--text); font: inherit; resize: vertical; }
  textarea:focus { outline: none; border-color: var(--accent); }
  .row { display: flex; justify-content: space-between; align-items: center; margin-top: 8px; }
  .it { display: flex; align-items: center; gap: 8px; padding: 8px 0; border-bottom: 1px solid var(--border); }
  .it:last-child { border-bottom: 0; }
  .body { flex: 1; min-width: 0; display: grid; gap: 2px; }
  .team { color: var(--accent-text); }
  details { padding: 5px 0; border-bottom: 1px solid var(--border); }
  summary { cursor: pointer; font-size: 12.5px; }
  pre { margin: 6px 0; padding: 8px 10px; font: 11px/1.5 var(--mono); white-space: pre-wrap; overflow-wrap: anywhere; background: var(--code-bg); border-radius: var(--r); }
  h2 .faint { font-weight: 400; text-transform: none; letter-spacing: 0; }
</style>
