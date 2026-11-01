<script lang="ts">
  import { onMount } from 'svelte';
  import { app, openFile, selectNode } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import CopyButton from './CopyButton.svelte';
  import { SEVERITIES } from '../lib/theme';
  import type { RepoReport } from '../lib/types';

  let r = $state.raw<RepoReport | null>(null);
  let error = $state('');
  let query = $state('');
  onMount(() => { api.repo().then((d) => (r = d), (e) => (error = (e as Error).message)); });

  const name = (p: string) => p.slice(p.lastIndexOf('/') + 1);
  function openClass(node: string) {
    app.mode = 'review';
    app.showPanel = true;
    selectNode(node);
  }
  function openLine(file: string, line: number) {
    app.mode = 'review';
    app.showPanel = true;
    void openFile(file, null, line);
  }
  const eps = $derived((r?.endpoints ?? []).filter((e) => !query || `${e.service} ${e.route} ${e.class} ${e.handler}`.toLowerCase().includes(query.toLowerCase())));
  const maxOf = (m: Record<string, number>) => Math.max(1, ...Object.values(m));
  const sorted = (m: Record<string, number>) => Object.entries(m).sort((a, b) => b[1] - a[1]);
  const langs = $derived(r ? sorted(r.languages).slice(0, 6) : []);
</script>

<div class="page"><div class="page-in">
  <header>
    <h1>{r?.name ?? 'Repository'}</h1>
    <p>The repository as it is on {r?.branch ?? '…'}: its services, APIs, who works on what, where it changes and what it costs.</p>
  </header>
  {#if error}<div class="pcard">Could not analyse the repository: {error}</div>
  {:else if !r}<div class="empty"><span class="spinner"></span></div>
  {:else}
    <div class="cards">
      <div class="stat"><b>{r.services.filter((s) => s.kind === 'service').length}</b><span>services</span></div>
      <div class="stat"><b>{r.endpoints.length}</b><span>endpoints</span></div>
      <div class="stat"><b>{r.calls.length}</b><span>service calls (Feign)</span></div>
      <div class="stat"><b>{r.types}</b><span>classes</span></div>
      <div class="stat"><b>{r.methods}</b><span>methods</span></div>
      <div class="stat"><b>{r.lines.toLocaleString()}</b><span>lines of Java</span></div>
      <div class="stat"><b>{r.commits}</b><span>commits</span></div>
      <div class="stat"><b>{r.contributors.length}</b><span>people</span></div>
    </div>

    <section class="pcard">
      <h2>Services</h2>
      <div style="overflow-x:auto"><table class="tbl">
        <thead><tr><th>Service</th><th>Port</th><th class="num">Classes</th><th class="num">Lines</th><th class="num">Tests</th><th class="num">Endpoints</th><th>Calls</th><th>Called by</th><th>Open issues</th><th class="num">Changes</th></tr></thead>
        <tbody>
          {#each r.services as s}
            <tr>
              <td class="mono">{s.name}{#if s.kind === 'library'} <span class="faint">library</span>{/if}</td>
              <td class="mono">{s.port ?? '—'}</td>
              <td class="num">{s.types}</td><td class="num">{s.lines}</td>
              <td class="num" class:warn={s.tests === 0 && s.kind === 'service'}>{s.tests}</td>
              <td class="num">{s.endpoints}</td>
              <td class="small">{s.calls.join(', ') || '—'}</td>
              <td class="small">{s.calledBy.join(', ') || '—'}</td>
              <td>{#each SEVERITIES as sv}{#if s.open[sv]}<span class="sev {sv}">{s.open[sv]} {sv}</span> {/if}{/each}{#if !Object.keys(s.open).length}<span class="faint">none</span>{/if}</td>
              <td class="num">{s.commits}</td>
            </tr>
          {/each}
        </tbody>
      </table></div>
    </section>

    <div class="split">
      <section class="pcard">
        <h2>Open issues by type</h2>
        {#each sorted(r.issues) as [k, n]}<div class="bar-row"><span>{k}</span><i style:width="{(n / maxOf(r.issues)) * 100}%"></i><b>{n}</b></div>{:else}<p class="faint">No open issues tracked yet. Commits get reviewed as they arrive.</p>{/each}
        {#if Object.keys(r.urgency).length}
          <h2 style="margin-top:12px">By urgency</h2>
          {#each sorted(r.urgency) as [k, n]}<div class="bar-row"><span>{k}</span><i style:width="{(n / maxOf(r.urgency)) * 100}%"></i><b>{n}</b></div>{/each}
        {/if}
      </section>
      <section class="pcard">
        <h2>People</h2>
        {#each r.contributors as c}<div class="bar-row"><span>{c.name}</span><i style:width="{(c.commits / Math.max(1, r.contributors[0].commits)) * 100}%"></i><b>{c.commits}</b></div>{/each}
        <h2 style="margin-top:12px">Files</h2>
        <p class="small muted">{r.files} files, {r.javaFiles} Java, {r.testFiles} test classes ({r.javaFiles ? Math.round((r.testFiles / r.javaFiles) * 100) : 0}%). {langs.map(([k, n]) => `${k} ${n}`).join(' · ')}</p>
      </section>

      <section class="pcard">
        <h2>Hotspots <span class="faint">changed often, with open issues</span></h2>
        <table class="tbl"><thead><tr><th>File</th><th class="num">Changes</th><th class="num">Issues</th><th class="num">Lines</th></tr></thead><tbody>
          {#each r.hotspots as h}<tr><td><button class="lnk mono small" title={h.path} onclick={() => openLine(h.path, 1)}>{name(h.path)}</button></td><td class="num">{h.commits}</td><td class="num">{h.open}</td><td class="num">{h.lines}</td></tr>{/each}
        </tbody></table>
      </section>
      <section class="pcard">
        <h2>Costly methods <span class="faint">measured from the code</span></h2>
        <table class="tbl"><thead><tr><th>Method</th><th>Cost</th><th class="num">Complexity</th><th class="num">I/O in loops</th></tr></thead><tbody>
          {#each r.costly as m}<tr><td><button class="lnk mono small" onclick={() => openLine(m.file, m.line)}>{m.class}.{m.method}()</button></td><td class="mono small">{m.cost.estimate}</td><td class="num">{m.cost.complexity}</td><td class="num">{m.cost.loopIo}</td></tr>{:else}<tr><td colspan="4" class="faint">Nothing stands out.</td></tr>{/each}
        </tbody></table>
      </section>
    </div>

    <section class="pcard">
      <h2>Service calls</h2>
      <table class="tbl"><thead><tr><th>From</th><th>To</th><th>Client</th><th>Routes</th></tr></thead><tbody>
        {#each r.calls as c}<tr><td class="mono small">{c.from}</td><td class="mono small">{c.to}</td><td><button class="lnk mono small" onclick={() => openClass(c.node)}>{c.client}</button></td><td class="mono small">{c.routes.join(', ')}</td></tr>{:else}<tr><td colspan="4" class="faint">No Feign clients.</td></tr>{/each}
      </tbody></table>
    </section>

    <section class="pcard">
      <h2>API endpoints · {r.endpoints.length}</h2>
      <input class="filter" bind:value={query} placeholder="Filter by service, route or class…" spellcheck="false" />
      <table class="tbl"><thead><tr><th>Service</th><th>Route</th><th>Handler</th><th class="num">Internal callers</th></tr></thead><tbody>
        {#each eps as e}<tr><td class="mono small">{e.service}</td><td class="mono small">{e.route} <CopyButton text={e.route} title="Copy the route" /></td><td><button class="lnk mono small" onclick={() => openClass(e.node)}>{e.class}.{e.handler}</button></td><td class="num">{e.callers || '—'}</td></tr>{/each}
      </tbody></table>
    </section>

    <div class="split">
      <section class="pcard">
        <h2>Largest classes</h2>
        <table class="tbl"><tbody>{#each r.largest as c}<tr><td><button class="lnk mono small" onclick={() => openClass(c.Node)}>{c.Name}</button></td><td class="num">{c.Lines} lines</td><td class="num">{c.Members} members</td></tr>{/each}</tbody></table>
      </section>
      <section class="pcard">
        <h2>Constants · {r.constants.length}</h2>
        <table class="tbl"><tbody>{#each r.constants.slice(0, 20) as c}<tr><td><button class="lnk mono small" onclick={() => openLine(c.file, c.line)}>{c.class}.{c.name}</button></td><td class="mono small">{c.value}</td></tr>{:else}<tr><td class="faint">None.</td></tr>{/each}</tbody></table>
      </section>
    </div>
  {/if}
</div></div>

<style>
  .small { font-size: 11.5px; }
  .warn { color: var(--warn); }
  .filter { width: 100%; height: 28px; margin-bottom: 8px; padding: 0 9px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12px; }
  h2 .faint { font-weight: 400; text-transform: none; letter-spacing: 0; }
</style>
