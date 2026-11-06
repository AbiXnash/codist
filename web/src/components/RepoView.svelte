<script lang="ts">
  import { onMount } from 'svelte';
  import { app, openFile, reveal, selectNode } from '../lib/store.svelte';
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
    selectNode(node);
    reveal();
  }
  function openLine(file: string, line: number) {
    void openFile(file, null, line);
  }
  // Long tables draw a batch of rows and add more as you scroll, so 1,700 endpoints stay quick.
  const BATCH = 60;
  let shownEps = $state(BATCH);
  let shownCalls = $state(BATCH);
  let epsBox = $state<HTMLElement>();
  const nearEnd = (b: HTMLElement) => b.scrollHeight - b.scrollTop - b.clientHeight < 300;
  const searchable = $derived((r?.endpoints ?? []).map((e) => ({ e, text: `${e.service} ${e.route} ${e.class} ${e.handler}`.toLowerCase() })));
  const eps = $derived.by(() => {
    const q = query.trim().toLowerCase();
    return q ? searchable.filter((x) => x.text.includes(q)).map((x) => x.e) : (r?.endpoints ?? []);
  });
  function onFilter() {
    shownEps = BATCH;
    if (epsBox) epsBox.scrollTop = 0;
  }

  let pageEl = $state<HTMLElement>();
  let showTop = $state(false);
  const onPageScroll = () => { showTop = (pageEl?.scrollTop ?? 0) > 500; };
  function toTop() {
    const calm = matchMedia('(prefers-reduced-motion: reduce)').matches;
    pageEl?.scrollTo({ top: 0, behavior: calm ? 'auto' : 'smooth' });
  }

  function whyCostly(c: RepoReport['costly'][number]['cost']) {
    const why: string[] = [];
    if (c.loopIo > 0) why.push(`${c.loopIo} slow call${c.loopIo === 1 ? '' : 's'} repeated in a loop`);
    if (c.loopDepth >= 2) why.push(`loops nested ${c.loopDepth} deep`);
    if (c.complexity >= 8) why.push(`${c.complexity} paths through the code`);
    return why.join(', ');
  }
  const maxOf = (m: Record<string, number>) => Math.max(1, ...Object.values(m));
  const sorted = (m: Record<string, number>) => Object.entries(m).sort((a, b) => b[1] - a[1]);
  const langs = $derived(r ? sorted(r.languages).slice(0, 6) : []);
</script>

<div class="page" bind:this={pageEl} onscroll={onPageScroll}><div class="page-in">
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
      <p class="hint">Each service and shared library in the repository. <b>Test classes</b> is highlighted at 0. <b>Times files changed</b> adds up every commit's change to each file in the service, so one commit touching three files counts as 3.</p>
      <div class="tscroll" tabindex="0" role="region" aria-label="Services"><table class="tbl">
        <thead><tr><th>Service</th><th>Port</th><th class="num">Classes</th><th class="num">Lines of Java</th><th class="num">Test classes</th><th class="num">Endpoints</th><th>Services it calls</th><th>Called by</th><th>Open issues</th><th class="num">Times files changed</th></tr></thead>
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
        <div class="bars">{#each sorted(r.issues) as [k, n]}<div class="bar-row"><span>{k}</span><i style:width="{(n / maxOf(r.issues)) * 100}%"></i><b>{n}</b></div>{:else}<p class="faint">No open issues tracked yet. Commits get reviewed as they arrive.</p>{/each}</div>
        {#if Object.keys(r.urgency).length}
          <h2 style="margin-top:12px">By urgency</h2>
          {#each sorted(r.urgency) as [k, n]}<div class="bar-row"><span>{k}</span><i style:width="{(n / maxOf(r.urgency)) * 100}%"></i><b>{n}</b></div>{/each}
        {/if}
      </section>
      <section class="pcard">
        <h2>People</h2>
        <p class="hint">Commits by each person. This shows who knows which code, not how well anyone works.</p>
        <div class="bars">{#each r.contributors as c}<div class="bar-row"><span>{c.name}</span><i style:width="{(c.commits / Math.max(1, r.contributors[0].commits)) * 100}%"></i><b>{c.commits}</b></div>{/each}</div>
        <h2 style="margin-top:12px">Files</h2>
        <p class="small muted">{r.files} files, {r.javaFiles} Java, {r.testFiles} test classes ({r.javaFiles ? Math.round((r.testFiles / r.javaFiles) * 100) : 0}%). {langs.map(([k, n]) => `${k} ${n}`).join(' · ')}</p>
      </section>

      <section class="pcard">
        <h2>Hotspots</h2>
        <p class="hint">Files that change most, ranked higher when they also have open issues. Top {r.hotspots.length} shown.</p>
        <table class="tbl"><thead><tr><th>File</th><th class="num">Commits</th><th class="num">Open issues</th><th class="num">Lines</th></tr></thead><tbody>
          {#each r.hotspots as h}<tr><td><button class="lnk mono small" title={h.path} onclick={() => openLine(h.path, 1)}>{name(h.path)}</button></td><td class="num">{h.commits}</td><td class="num">{h.open}</td><td class="num">{h.lines}</td></tr>{/each}
        </tbody></table>
      </section>
      <section class="pcard">
        <h2>Largest classes</h2>
        <p class="hint">The biggest classes by line count. <b>Fields and methods</b> counts what each class declares. Top {r.largest.length} shown.</p>
        <table class="tbl"><thead><tr><th>Class</th><th class="num">Lines</th><th class="num">Fields and methods</th></tr></thead><tbody>{#each r.largest as c}<tr><td><button class="lnk mono small" onclick={() => openClass(c.Node)}>{c.Name}</button></td><td class="num">{c.Lines}</td><td class="num">{c.Members}</td></tr>{/each}</tbody></table>
      </section>
    </div>

    <section class="pcard">
      <h2>Costly methods <span class="count">{#if r.costlyTotal > r.costly.length}worst {r.costly.length} of {r.costlyTotal}{:else}{r.costly.length}{/if}</span></h2>
      <p class="hint">Estimated by reading the code. Nothing is run, so these are not timings, and a slow call hidden inside a helper method is missed. <b>Work as data grows</b>: O(1) stays the same, O(n) grows with the data, O(n²) grows much faster, and round trips means a database or remote call on every pass. These are worst cases: nested loops read as O(n²) even when they walk each item once, as tree walks do. <b>Paths</b> counts the routes through the method; more paths are harder to test. <b>Slow calls in loops</b> counts database and remote calls written inside a loop.</p>
      <div class="tscroll" tabindex="0" role="region" aria-label="Costly methods"><table class="tbl"><thead><tr><th>Method</th><th>Why it is listed</th><th>Work as data grows</th><th class="num">Paths</th><th class="num">Slow calls in loops</th></tr></thead><tbody>
        {#each r.costly as m}<tr><td><button class="lnk mono small" onclick={() => openLine(m.file, m.line)}>{m.class}.{m.method}()</button></td><td class="small">{whyCostly(m.cost)}</td><td class="mono small">{m.cost.estimate}</td><td class="num">{m.cost.complexity}</td><td class="num">{m.cost.loopIo}</td></tr>{:else}<tr><td colspan="5" class="faint">Nothing stands out: no slow calls in loops, no nested loops and no method with 8 or more paths.</td></tr>{/each}
      </tbody></table></div>
    </section>

    <section class="pcard">
      <h2>Service calls</h2>
      <p class="hint">How services talk to each other through Feign clients. Each row is one client: the service using it, the service it reaches, and the routes it calls.</p>
      <div class="tscroll" tabindex="0" role="region" aria-label="Service calls" onscroll={(ev) => { if (nearEnd(ev.currentTarget) && shownCalls < r!.calls.length) shownCalls += BATCH; }}><table class="tbl"><thead><tr><th>Calling service</th><th>Called service</th><th>Feign client</th><th>Routes it calls</th></tr></thead><tbody>
        {#each r.calls.slice(0, shownCalls) as c}<tr><td class="mono small">{c.from}</td><td class="mono small">{c.to}</td><td><button class="lnk mono small" onclick={() => openClass(c.node)}>{c.client}</button></td><td class="mono small">{c.routes.join(', ')}</td></tr>{:else}<tr><td colspan="4" class="faint">No Feign clients.</td></tr>{/each}
      </tbody></table></div>
    </section>

    <section class="pcard">
      <h2>API endpoints <span class="count">{#if query.trim()}{eps.length} of {r.endpoints.length}{:else}{r.endpoints.length}{/if}</span></h2>
      <p class="hint"><b>Handler</b> is the method that answers the route. <b>Feign callers</b> counts calls to it from services in this repository, so a dash can still mean a browser or an outside system uses it.</p>
      <input class="filter" bind:value={query} oninput={onFilter} placeholder="Filter by service, route or class…" spellcheck="false" aria-label="Filter endpoints" />
      <div class="tscroll" tabindex="0" role="region" aria-label="API endpoints" bind:this={epsBox} onscroll={(ev) => { if (nearEnd(ev.currentTarget) && shownEps < eps.length) shownEps += BATCH; }}><table class="tbl"><thead><tr><th>Service</th><th>Route</th><th>Handler</th><th class="num">Feign callers</th></tr></thead><tbody>
        {#each eps.slice(0, shownEps) as e}<tr><td class="mono small">{e.service}</td><td class="mono small">{e.route} <CopyButton text={e.route} title="Copy the route" /></td><td><button class="lnk mono small" onclick={() => openClass(e.node)}>{e.class}.{e.handler}</button></td><td class="num">{e.callers || '—'}</td></tr>{:else}<tr><td colspan="4" class="faint none">No endpoint matches "{query}".</td></tr>{/each}
      </tbody></table></div>
    </section>
  {/if}
</div>
{#if showTop}
  <button class="to-top" type="button" onclick={toTop}><span aria-hidden="true">↑</span> Back to top</button>
{/if}
</div>

<style>
  .small { font-size: 11.5px; }
  .warn { color: var(--warn); }
  .filter { width: 100%; height: 28px; margin-bottom: 8px; padding: 0 9px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12px; }
  .hint { margin: -6px 0 10px; max-width: 78ch; font-size: 12px; line-height: 1.45; color: var(--text-2); }
  .hint b { font-weight: 600; color: var(--text); }
  .count { font-weight: 400; color: var(--text-2); }
  .tscroll { max-height: 440px; overflow: auto; overscroll-behavior: contain; }
  .tscroll :global(thead th) { position: sticky; top: 0; z-index: 1; background: var(--surface); box-shadow: inset 0 -1px 0 var(--border); }
  .bars { max-height: 230px; overflow: auto; overscroll-behavior: contain; }
  .none { padding: 10px 8px; }
  .page { position: relative; }
  .to-top { position: fixed; right: 24px; bottom: 24px; z-index: 5; height: 32px; padding: 0 12px; border: 1px solid var(--border); border-radius: 16px; background: var(--surface); color: var(--text); font: inherit; font-size: 12px; box-shadow: 0 2px 10px rgba(0, 0, 0, .18); cursor: pointer; }
  .to-top:hover { background: var(--surface-2); }
</style>
