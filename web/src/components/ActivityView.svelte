<script lang="ts">
  import { onMount } from 'svelte';
  import { app, openFile, openIncident, showCommit } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import CopyButton from './CopyButton.svelte';
  import { ago } from '../lib/theme';
  import type { Activity } from '../lib/types';

  // Everything Codist did and everything the model saw and said, live.
  let items = $state.raw<Activity[]>([]);
  let picked = $state.raw<Activity | null>(null);
  let pickedId = $state<string | null>(null);
  let kind = $state('');
  let status = $state('');
  let query = $state('');
  let aiOnly = $state(false);
  let view = $state<'prompt' | 'system' | 'response'>('prompt');
  let failed = $state('');

  async function load() {
    try { items = await api.activity(); failed = ''; } catch (e) { failed = (e as Error).message; }
  }
  async function pick(id: string) {
    pickedId = id;
    picked = await api.activityItem(id);
  }
  onMount(() => {
    void load();
    const on = (e: Event) => {
      const a = (e as CustomEvent<Activity>).detail;
      const i = items.findIndex((x) => x.id === a.id);
      items = i >= 0 ? items.map((x, k) => (k === i ? a : x)) : [a, ...items].slice(0, 300);
      if (a.id === pickedId) void pick(a.id);
    };
    window.addEventListener('codist:activity', on);
    return () => window.removeEventListener('codist:activity', on);
  });

  const kinds = $derived([...new Set(items.map((a) => a.kind))].sort());
  const shown = $derived(items.filter((a) => (!kind || a.kind === kind) && (!status || a.status === status) && (!aiOnly || a.kind !== 'step') &&
    (!query || `${a.title} ${a.path ?? ''} ${a.sha ?? ''} ${a.note ?? ''}`.toLowerCase().includes(query.toLowerCase()))));
  const totals = $derived(items.reduce((t, a) => ({ calls: t.calls + (a.kind === 'step' ? 0 : 1), tokens: t.tokens + (a.promptTokens ?? 0) + (a.replyTokens ?? 0), ms: t.ms + (a.kind === 'step' ? 0 : a.tookMs) }), { calls: 0, tokens: 0, ms: 0 }));
  const secs = (ms: number) => (ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${ms} ms`);

  // The prompt split into its "## " sections, so each part can be read on its own.
  const sections = $derived.by(() => {
    const text = picked?.user ?? '';
    const out: { title: string; body: string }[] = [];
    for (const part of text.split(/\n(?=## )/)) {
      const nl = part.indexOf('\n');
      if (part.startsWith('## ') && nl > 0) out.push({ title: part.slice(3, nl), body: part.slice(nl + 1) });
      else if (part.trim()) out.push({ title: 'Prompt', body: part });
    }
    return out;
  });
  const pretty = $derived.by(() => {
    const r = picked?.response ?? '';
    try { return JSON.stringify(JSON.parse(r), null, 2); } catch { return r; }
  });
  function goCommit(sha: string) { void showCommit(sha); }
  function goFile(p: string) { void openFile(p, null); }
  function goIncident(id: string) { app.mode = 'incidents'; void openIncident(id); }
</script>

<div class="wrap">
  <aside>
    <div class="top">
      <h1>Activity</h1>
      <p class="faint small">{totals.calls} model calls · {totals.tokens.toLocaleString()} tokens · {secs(totals.ms)} of model time</p>
      <input bind:value={query} placeholder="Search title, file, commit…" spellcheck="false" />
      <div class="row">
        <select bind:value={kind} aria-label="Kind"><option value="">All kinds</option>{#each kinds as k}<option value={k}>{k}</option>{/each}</select>
        <select bind:value={status} aria-label="Status"><option value="">Any status</option><option value="running">Running</option><option value="done">Done</option><option value="failed">Failed</option><option value="cancelled">Cancelled</option></select>
      </div>
      <label class="small"><input type="checkbox" bind:checked={aiOnly} /> Model calls only</label>
    </div>
    <ul>
      {#each shown as a (a.id)}
        <li>
          <button class="item" class:on={a.id === pickedId} onclick={() => pick(a.id)}>
            <span class="dot {a.status}" class:spin={a.status === 'running'}></span>
            <span class="t">{a.title}</span>
            <span class="meta"><span class="kind">{a.kind}</span>{#if a.sha}<span class="mono">{a.sha.slice(0, 7)}</span>{/if}{#if a.tookMs}<span>{secs(a.tookMs)}</span>{/if}<span class="time">{ago(a.at)}</span></span>
            {#if a.note}<span class="note">{a.note}</span>{/if}
          </button>
        </li>
      {:else}<li class="empty">{failed || 'Nothing yet. Reviews, fixes and questions show up here as they happen.'}</li>{/each}
    </ul>
  </aside>

  <main>
    {#if !picked}
      <div class="empty">Pick an entry to see exactly what was sent to the model and what it answered.</div>
    {:else}
      <header>
        <div class="title"><span class="dot {picked.status}"></span><h2>{picked.title}</h2></div>
        <div class="facts">
          <span class="kind">{picked.kind}</span><span>{picked.status}</span>
          {#if picked.model}<span class="mono">{picked.model}</span>{/if}
          {#if picked.tookMs}<span>{secs(picked.tookMs)}</span>{/if}
          {#if picked.promptTokens}<span>{picked.promptTokens} tokens in, {picked.replyTokens} out</span>{/if}
          <span class="faint">{new Date(picked.at).toLocaleString()}</span>
        </div>
        {#if picked.sha || picked.path || picked.incident || picked.knowledge?.length}
          <div class="rel">
            <span class="label">Related</span>
            {#if picked.sha}<button class="lnk mono" onclick={() => goCommit(picked!.sha!)}>commit {picked.sha.slice(0, 7)}</button>{/if}
            {#if picked.path}<button class="lnk mono" onclick={() => goFile(picked!.path!)}>{picked.path.slice(picked.path.lastIndexOf('/') + 1)}</button>{/if}
            {#if picked.incident}<button class="lnk" onclick={() => goIncident(picked!.incident!)}>incident</button>{/if}
            {#each picked.knowledge ?? [] as k}<span class="kn mono" title="Knowledge entry retrieved for this prompt">{k}</span>{/each}
          </div>
        {/if}
        {#if picked.note}<p class="muted">{picked.note}</p>{/if}
        {#if picked.error}<p class="err">{picked.error} <CopyButton text={picked.error} title="Copy the error" /></p>{/if}
      </header>
      {#if picked.kind !== 'step'}
        <div class="tabsrow"><div class="seg" role="tablist">
          <button class:on={view === 'prompt'} onclick={() => (view = 'prompt')}>Fed to the AI · {sections.length} sections</button>
          <button class:on={view === 'system'} onclick={() => (view = 'system')}>Instructions</button>
          <button class:on={view === 'response'} onclick={() => (view = 'response')}>Response</button>
        </div><CopyButton text={picked.user ?? ''} label="Copy prompt" /></div>
        <div class="content">
          {#if view === 'prompt'}
            {#each sections as s, i}
              <details open={i < 4}><summary>{s.title} <span class="faint">{s.body.length.toLocaleString()} chars</span> <CopyButton text={s.body} title="Copy this section" /></summary><pre>{s.body}</pre></details>
            {/each}
          {:else if view === 'system'}
            <div class="cp"><CopyButton text={picked.system ?? ''} label="Copy instructions" /></div>
            <pre>{picked.system}</pre>
          {:else if picked.status === 'running'}
            <div class="empty"><span class="spinner"></span> The model is working…</div>
          {:else}
            <div class="cp"><CopyButton text={pretty} label="Copy response" /></div>
            <pre>{pretty || '(no response)'}</pre>
          {/if}
        </div>
      {:else}
        <p class="faint">This step ran without the model, so there is no prompt.</p>
      {/if}
    {/if}
  </main>
</div>

<style>
  .wrap { flex: 1; min-height: 0; display: grid; grid-template-columns: 370px minmax(0, 1fr); background: var(--bg); }
  aside { display: flex; flex-direction: column; min-height: 0; border-right: 1px solid var(--border); background: var(--surface); }
  .top { display: grid; gap: 7px; padding: 12px; border-bottom: 1px solid var(--border); }
  h1 { margin: 0; font-size: 16px; font-weight: 600; }
  .small { font-size: 11.5px; margin: 0; }
  input:not([type]), select { min-width: 0; height: 28px; padding: 0 8px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12px; }
  .row { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; }
  ul { list-style: none; margin: 0; padding: 4px; overflow: auto; flex: 1; }
  .item { display: grid; grid-template-columns: 12px minmax(0, 1fr); gap: 2px 7px; width: 100%; padding: 7px 8px; border: 0; border-radius: var(--r); background: none; text-align: left; }
  .item:hover { background: var(--hover); }
  .item.on { background: var(--accent-soft); }
  .t { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12.5px; }
  .meta { grid-column: 2; display: flex; gap: 8px; font-size: 11px; color: var(--text-3); align-items: center; }
  .note { grid-column: 2; font-size: 11px; color: var(--text-2); }
  .time { margin-left: auto; white-space: nowrap; }
  .kind { font: 600 10px var(--font); padding: 0 5px; line-height: 15px; border-radius: 4px; background: var(--surface-3); color: var(--text-2); }
  .dot { width: 8px; height: 8px; margin-top: 5px; border-radius: 50%; background: var(--good); display: inline-block; flex-shrink: 0; }
  .dot.failed { background: var(--bad); } .dot.cancelled { background: var(--text-3); } .dot.running { background: none; border: 1.5px solid var(--accent); border-right-color: transparent; }
  .dot.spin { animation: spin 0.8s linear infinite; }
  main { min-height: 0; min-width: 0; overflow: auto; padding: 16px 20px 48px; display: flex; flex-direction: column; gap: 10px; }
  .title { display: flex; align-items: center; gap: 8px; }
  h2 { margin: 0; font-size: 15px; font-weight: 600; }
  .facts { display: flex; flex-wrap: wrap; gap: 4px 12px; margin-top: 5px; font-size: 12px; color: var(--text-2); align-items: center; }
  .rel { display: flex; flex-wrap: wrap; gap: 4px 10px; margin-top: 8px; align-items: center; font-size: 12px; }
  .kn { font-size: 10.5px; padding: 0 6px; line-height: 16px; border-radius: 8px; color: var(--accent-text); background: var(--accent-soft); }
  .err { margin: 6px 0 0; color: var(--bad); }
  header p { margin: 6px 0 0; }
  .seg { display: flex; align-self: flex-start; padding: 2px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .seg button { height: 26px; padding: 0 10px; border: 0; border-radius: var(--r); background: none; font-size: 12px; color: var(--text-3); }
  .seg button.on { color: var(--text); background: var(--surface-3); box-shadow: var(--raised); }
  .tabsrow { display: flex; align-items: center; gap: 8px; }
  .cp { justify-self: start; }
  .content { display: grid; gap: 6px; min-width: 0; }
  details { border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface); min-width: 0; }
  summary { padding: 7px 10px; cursor: pointer; font-size: 12.5px; font-weight: 500; }
  pre { margin: 0; padding: 10px 12px; font: 11.5px/1.5 var(--mono); white-space: pre-wrap; overflow-wrap: anywhere; background: var(--code-bg); border-radius: var(--r); }
  @media (max-width: 900px) { .wrap { grid-template-columns: 1fr; } }
</style>
