<script lang="ts">
  import { onMount } from 'svelte';
  import { app, openFile, toast } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import { ago, SEVERITIES } from '../lib/theme';
  import type { DependencyReport, Issue, ReleaseReport, SignOff } from '../lib/types';

  let report = $state.raw<ReleaseReport | null>(null);
  let markdown = $state('');
  let refs = $state.raw<{ ref: string; label: string }[]>([]);
  let deps = $state.raw<DependencyReport | null>(null);
  let signoff = $state.raw<SignOff | null>(null);
  let to = $state('');
  let showAll = $state(false);
  let loading = $state(false);
  let newItem = $state('');

  async function load() {
    loading = true;
    try {
      const d = await api.release(to || undefined);
      report = d.report;
      signoff = d.signoff;
      markdown = d.markdown;
      refs = d.refs;
      if (!to) to = d.report.to;
    } catch (e) {
      toast((e as Error).message, 'bad');
    } finally {
      loading = false;
    }
  }
  onMount(() => {
    void load();
    api.dependencies().then((d) => (deps = d), () => {});
    // Someone ticked an item (here or in another tab): reload the shared sign-off.
    const onSign = (e: Event) => {
      if ((e as CustomEvent<{ to: string }>).detail.to === report?.to) void load();
    };
    window.addEventListener('codist:signoff', onSign);
    return () => window.removeEventListener('codist:signoff', onSign);
  });

  async function tick(text: string, done: boolean, custom = false) {
    if (!report) return;
    try {
      await api.signOff(report.to, text, done, custom);
      await load();
    } catch (e) {
      toast((e as Error).message, 'bad');
    }
  }
  async function addItem() {
    const t = newItem.trim();
    if (!t) return;
    newItem = '';
    await tick(t, false, true);
  }
  async function removeItem(id: string) {
    if (!report) return;
    await api.removeSignOff(report.to, id);
    await load();
  }

  const name = (p: string) => p.slice(p.lastIndexOf('/') + 1);
  const short = (s?: string) => (s ? s.slice(0, 7) : '');
  function goToIssue(is: Issue) {
    if (!report) return;
    app.mode = 'review';
    void openFile(is.path, report.to, is.line);
  }
  function copy() {
    navigator.clipboard?.writeText(markdown).then(() => toast('Report copied as Markdown', 'good', 1500), () => toast('Copy was blocked', 'warn'));
  }
  const count = (sev: string) => report?.open.filter((i) => i.severity === sev).length ?? 0;
  const shown = $derived(report ? (showAll ? report.open : report.open.slice(0, 25)) : []);
</script>

<div class="release">
  <div class="inner">
    <header>
      <div>
        <h1>Release risk report</h1>
        <p class="muted">The whole codebase at the release commit: every file checked, every open issue, and what to verify before sign-off.</p>
      </div>
      <div class="pick">
        <label class="label" for="rel-to">Release</label>
        <select id="rel-to" value={to} onchange={(e) => { to = e.currentTarget.value; showAll = false; void load(); }}>
          {#each refs as r}<option value={r.ref}>{r.label}</option>{/each}
        </select>
        <button class="btn secondary sm" onclick={copy} disabled={!markdown}>Copy as Markdown</button>
      </div>
    </header>

    {#if loading && !report}
      <div class="empty"><span class="spinner"></span><span class="faint">Checking every file…</span></div>
    {:else if report}
      <section class="verdict {report.risk}">
        <div class="risk">{report.risk} risk</div>
        <ul>{#each report.reasons as r}<li>{r}</li>{:else}<li>No open issues or build problems in the code.</li>{/each}</ul>
        <div class="stats">
          <span><b>{report.files}</b> files</span>
          <span><b>{report.lines.toLocaleString()}</b> lines</span>
          <span><b>{report.open.length}</b> open issues</span>
          {#each SEVERITIES as sv}{#if count(sv)}<span class="sev {sv}">{count(sv)} {sv}</span>{/if}{/each}
          {#if report.ignored}<span><b>{report.ignored}</b> ignored</span>{/if}
          <span><b>{report.reviewed}</b> / {report.sources} source files reviewed by the model</span>
        </div>
      </section>

      <div class="grid">
        <section>
          <div class="so-head">
            <h2>Sign-off for <span class="mono">{report.to.slice(0, 7)}</span></h2>
            {#if signoff}<span class="progress" class:complete={signoff.total > 0 && signoff.done === signoff.total}>{signoff.done} / {signoff.total} signed off</span>{/if}
          </div>
          <p class="faint small hint">One checklist per release commit, built from the code as it is and shared with everyone.</p>
          {#each signoff?.items ?? [] as it (it.id)}
            <div class="check" class:stale={!it.current}>
              <input id="so-{it.id}" type="checkbox" checked={it.done} disabled={!it.current} onchange={(e) => tick(it.text, e.currentTarget.checked, it.custom)} />
              <label for="so-{it.id}">
                <span class:done={it.done}>{it.text}</span>
                {#if it.done}<span class="who">✓ {it.by || 'someone'} · {ago(it.at ?? 0)}</span>{/if}
                {#if !it.current}<span class="who">no longer needed for this code; kept because it was signed off</span>{/if}
              </label>
              {#if it.custom}<button class="x" title="Remove this item" aria-label="Remove item" onclick={() => removeItem(it.id)}>×</button>{/if}
            </div>
          {/each}
          <form class="add" onsubmit={(e) => { e.preventDefault(); void addItem(); }}>
            <input id="so-new" bind:value={newItem} placeholder="Add an item, e.g. Notify the support team" />
            <button class="btn secondary sm" disabled={!newItem.trim()}>Add</button>
          </form>
        </section>

        <section>
          <h2>Open issues · {report.open.length}</h2>
          {#each shown as is (is.id)}
            <button class="row issue" onclick={() => goToIssue(is)} title={is.detail}>
              <span class="sev {is.severity}">{is.severity}</span>
              <span class="t">{is.title}</span>
              <span class="mono faint">{name(is.path)}:{is.line}</span>
              {#if is.firstBy}<span class="faint by">{is.firstBy} · <span class="mono">{short(is.firstSeen)}</span></span>{/if}
            </button>
          {:else}<p class="faint">None.</p>{/each}
          {#if report.open.length > shown.length}
            <button class="btn secondary sm more" onclick={() => (showAll = true)}>Show all {report.open.length}</button>
          {/if}
        </section>

        <section>
          <h2>Modules</h2>
          {#each report.modules as m}
            <div class="row"><span class="mono t">{m.name}</span><span class="faint">{m.files} file{m.files === 1 ? '' : 's'}</span>
              <span class="n" class:zero={!m.issues}>{m.issues} issue{m.issues === 1 ? '' : 's'}</span></div>
          {:else}<p class="faint">No Java sources.</p>{/each}
          {#if report.buildFindings.length}
            <h2 class="sub">Build</h2>
            {#each report.buildFindings as f}<div class="row"><span class="sev {f.severity}">{f.severity}</span><span class="t">{f.title}</span></div>{/each}
          {/if}
        </section>

        <section>
          <h2>Not reviewed by the model · {report.notReviewed.length}</h2>
          <p class="faint small hint">Rule checks ran on every file. These source files have never been through an AI review, so only the rules have looked at them.</p>
          {#each report.notReviewed as p}
            <button class="row issue" onclick={() => { app.mode = 'review'; void openFile(p, report!.to); }}><span class="mono t">{p}</span></button>
          {:else}<p class="faint">Every source file has been reviewed.</p>{/each}
        </section>
      </div>
    {/if}

    {#if deps}
      <section class="deps">
        <h2>Dependencies · {deps.file || 'no build file'}</h2>
        <p class="muted small">
          {deps.tool}{deps.springBoot ? ` · Spring Boot ${deps.springBoot}` : ''}{deps.java ? ` · Java ${deps.java}` : ''}
          {#if deps.support}<span class="warnc"> · {deps.support}</span>{/if}
        </p>
        <div class="table">
          <table>
            <thead><tr><th>Dependency</th><th>Version</th><th>Scope</th><th>Status</th></tr></thead>
            <tbody>
              {#each deps.deps as d}
                <tr>
                  <td class="mono">{d.artifact}<div class="faint small">{d.group}</div></td>
                  <td class="mono small">{d.version || '—'}</td>
                  <td>{d.scope}</td>
                  <td><span class="st {d.status}">{d.status === 'ok' ? 'ok' : d.status === 'warn' ? 'check' : 'upgrade'}</span>{#if d.note}<div class="small note">{d.note}</div>{/if}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </section>
    {/if}
  </div>
</div>

<style>
  .release { flex: 1; min-height: 0; overflow: auto; background: var(--bg); }
  .inner { max-width: 1120px; margin: 0 auto; padding: 22px 24px 72px; display: grid; gap: 18px; }
  header { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; flex-wrap: wrap; }
  h1 { margin: 0 0 4px; font-size: 20px; font-weight: 600; }
  header p { margin: 0; }
  h2 { margin: 0 0 8px; font-size: 13px; font-weight: 600; color: var(--text-2); text-transform: uppercase; letter-spacing: 0.04em; }
  h2.sub { margin-top: 14px; }
  .pick { display: flex; align-items: center; gap: 8px; }
  select { max-width: 340px; height: 28px; padding: 0 8px; border: 1px solid var(--border-strong); border-radius: var(--r); background: var(--surface); color: var(--text); font: inherit; font-size: 12.5px; }
  .verdict { padding: 14px 16px; border: 1px solid var(--border); border-left: 3px solid var(--c); border-radius: var(--r-lg); background: var(--surface); display: grid; gap: 6px; }
  .verdict.low { --c: var(--good); } .verdict.medium { --c: var(--warn); } .verdict.high { --c: var(--bad); }
  .risk { font-size: 16px; font-weight: 600; text-transform: capitalize; color: var(--c); }
  .verdict ul { margin: 0; padding-left: 18px; color: var(--text-2); }
  .stats { display: flex; gap: 18px; flex-wrap: wrap; font-size: 12.5px; color: var(--text-2); margin-top: 4px; }
  .stats b { color: var(--text); font-variant-numeric: tabular-nums; }
  .grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px; }
  section { min-width: 0; }
  .grid section { padding: 14px 16px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface); }
  .row { display: flex; align-items: center; gap: 8px; padding: 4px 0; min-width: 0; font-size: 12.5px; }
  .t { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ok { color: var(--good); }
  .kind { font-size: 10.5px; padding: 0 5px; border-radius: 3px; background: var(--surface-3); color: var(--text-2); text-transform: uppercase; }
  .so-head { display: flex; justify-content: space-between; align-items: baseline; gap: 8px; }
  .so-head h2 { margin: 0; }
  .progress { font-size: 12px; font-weight: 600; color: var(--warn); }
  .progress.complete { color: var(--good); }
  .hint { margin: 4px 0 8px; }
  .check { display: flex; gap: 8px; align-items: baseline; padding: 5px 0; font-size: 13px; }
  .check label { display: grid; gap: 1px; cursor: pointer; flex: 1; min-width: 0; }
  .check .done { text-decoration: line-through; color: var(--text-3); }
  .check.stale { opacity: 0.6; }
  .who { font-size: 11.5px; color: var(--good); }
  .check.stale .who { color: var(--text-3); }
  .x { border: 0; background: none; color: var(--text-3); font-size: 16px; line-height: 1; padding: 0 4px; }
  .x:hover { color: var(--bad); }
  .add { display: flex; gap: 6px; margin-top: 8px; }
  .add input { flex: 1; min-width: 0; height: 26px; padding: 0 8px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12.5px; }
  .add input:focus { outline: none; border-color: var(--accent); }
  .issue { width: 100%; border: 0; border-radius: var(--r); background: none; text-align: left; padding: 4px; color: inherit; font: inherit; font-size: 12.5px; }
  .issue:hover { background: var(--hover); }
  .by { font-size: 11.5px; white-space: nowrap; }
  .n { font-size: 11.5px; color: var(--warn); white-space: nowrap; }
  .n.zero { color: var(--text-3); }
  .more { margin-top: 8px; }
  .grid section:nth-child(2), .grid section:nth-child(4) { max-height: 460px; overflow: auto; }
  .empty { display: flex; gap: 10px; align-items: center; justify-content: center; padding: 40px; }
  .warnc { color: var(--warn); font-size: 11.5px; white-space: nowrap; }
  .small { font-size: 11.5px; }
  .deps .table { overflow-x: auto; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface); }
  table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
  th, td { text-align: left; padding: 8px 12px; border-bottom: 1px solid var(--border); vertical-align: top; }
  th { font-size: 11px; font-weight: 600; color: var(--text-3); text-transform: uppercase; letter-spacing: 0.04em; background: var(--surface-2); }
  tr:last-child td { border-bottom: 0; }
  .st { font-size: 11px; font-weight: 600; padding: 1px 7px; border-radius: 4px; }
  .st.ok { color: var(--good); background: color-mix(in srgb, var(--good) 12%, transparent); }
  .st.warn { color: var(--warn); background: color-mix(in srgb, var(--warn) 12%, transparent); }
  .st.bad { color: var(--bad); background: color-mix(in srgb, var(--bad) 12%, transparent); }
  .note { margin-top: 4px; color: var(--text-2); }
  @media (max-width: 900px) { .grid { grid-template-columns: 1fr; } }
</style>
