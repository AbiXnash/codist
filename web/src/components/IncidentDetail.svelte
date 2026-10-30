<script lang="ts">
  import { app, loadIncidents, openFile, openIncident, openProposal, selectCommit, selectNode, toast } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import { ago } from '../lib/theme';
  import type { IncidentLocation, RcaReport } from '../lib/types';

  const inc = $derived(app.incident!);
  const short = (s?: string) => (s ? s.slice(0, 7) : '');
  const name = (p?: string) => (p ? p.slice(p.lastIndexOf('/') + 1) : '');
  const occurrences = $derived(inc.errors.reduce((n, e) => n + e.count, 0));
  const maxScore = $derived(Math.max(1, ...inc.suspects.map((s) => s.score)));

  // Editable report and runbook, re-seeded when another incident opens.
  let report = $state<RcaReport>({ summary: '', timeline: '', impact: '', rootCause: '', fix: '', prevention: '' });
  let runbook = $state('');
  let seeded = '';
  $effect(() => {
    if (inc.id !== seeded) {
      seeded = inc.id;
      report = { ...inc.report };
      runbook = inc.runbook ?? '';
      rule = null;
    }
  });

  let busy = $state<string | null>(null);
  let rule = $state<{ cause: number; yaml: string; team: boolean } | null>(null);

  function goToCode(l: IncidentLocation) {
    app.mode = 'review';
    app.showPanel = true;
    selectNode(l.nodeId);
    void openFile(l.path, null, l.line);
  }
  function goToCommit(sha: string) {
    app.mode = 'review';
    void selectCommit(sha, true);
    app.tab = 'review';
  }
  async function confirm(i: number) {
    const d = await api.updateIncident(inc.id, { confirmed: inc.confirmed === i ? -1 : i });
    app.incident = d.incident;
    app.incidentMarkdown = d.markdown;
    void loadIncidents();
  }
  async function saveReport() {
    const d = await api.updateIncident(inc.id, { report });
    app.incident = d.incident;
    app.incidentMarkdown = d.markdown;
    toast('Report saved', 'good', 1500);
  }
  function copy(text: string, what: string) {
    navigator.clipboard?.writeText(text).then(() => toast(`${what} copied`, 'good', 1500), () => toast('Copy was blocked; select the text instead', 'warn'));
  }
  async function draftRule(i: number) {
    busy = 'rule';
    try {
      const { yaml } = await api.incidentRule(inc.id, i);
      rule = { cause: i, yaml, team: true };
    } catch (e) {
      toast((e as Error).message, 'bad');
    } finally {
      busy = null;
    }
  }
  async function saveRule() {
    if (!rule) return;
    try {
      const r = await api.saveRule(rule.yaml, rule.team);
      toast(`Rule saved to ${r.file}; Codist now checks ${r.total} rules`, 'good', 6000);
      rule = null;
    } catch (e) {
      toast((e as Error).message, 'bad', 6000);
    }
  }
  async function draftRunbook() {
    busy = 'runbook';
    try {
      runbook = (await api.incidentRunbook(inc.id)).markdown;
    } catch (e) {
      toast((e as Error).message, 'bad');
    } finally {
      busy = null;
    }
  }
  async function saveRunbook() {
    await api.updateIncident(inc.id, { runbook });
    toast('Runbook saved', 'good', 1500);
  }
  async function remove() {
    await api.deleteIncident(inc.id);
    await loadIncidents();
    await openIncident(null);
  }
  const sections: [keyof RcaReport, string][] = [
    ['summary', 'Summary'], ['timeline', 'Timeline'], ['impact', 'Impact'], ['rootCause', 'Root cause'], ['fix', 'Fix'], ['prevention', 'Prevention'],
  ];
</script>

<div class="detail">
  <header>
    <div class="eyebrow">Incident · {ago(inc.createdAt)} · {occurrences} occurrence{occurrences === 1 ? '' : 's'} in {(inc.logBytes / 1024).toFixed(0)} KB of logs</div>
    <h1>{inc.title}</h1>
    <div class="status">
      {#if inc.status === 'analysing'}<span class="spinner"></span> Analysing with {inc.model}… the evidence below fills in first.
      {:else if inc.status === 'confirmed'}<span class="pill ok">Root cause confirmed</span>
      {:else if inc.status === 'failed'}<span class="pill bad">Analysis failed</span>
      {:else}<span class="pill">Analysis ready</span>{/if}
      <span class="spacer"></span>
      <button class="btn secondary sm" onclick={() => copy(app.incidentMarkdown, 'RCA report')}>Copy RCA report</button>
      <button class="btn secondary sm" onclick={remove}>Delete</button>
    </div>
    {#if inc.error}<p class="warn">{inc.error}</p>{/if}
  </header>

  <section>
    <h2>Likely root cause</h2>
    {#each inc.causes as c, i}
      <article class="cause" class:confirmed={inc.confirmed === i}>
        <div class="cause-head">
          <span class="conf {c.confidence}">{c.confidence} confidence</span>
          <h3>{c.title}</h3>
          {#if c.sha}<button class="ref mono" onclick={() => goToCommit(c.sha!)} title="Introduced in this commit">{short(c.sha)}</button>{/if}
        </div>
        <p>{c.explanation}</p>
        {#if c.evidence.length}
          <ul class="evidence">
            {#each c.evidence as ev}
              <li><span class="kind {ev.kind}">{ev.kind}</span><span class="mono ev">{ev.text}</span>{#if ev.path}<span class="faint mono">{name(ev.path)}{ev.line ? ':' + ev.line : ''}</span>{/if}</li>
            {/each}
          </ul>
        {/if}
        {#if c.fix}<p class="fixline"><b>Fix:</b> {c.fix}</p>{/if}
        <div class="row">
          <button class="btn sm" class:secondary={inc.confirmed === i} onclick={() => confirm(i)}>{inc.confirmed === i ? 'Unconfirm' : 'Confirm root cause'}</button>
          {#if c.path && app.status?.canFix}
            <button class="btn secondary sm" onclick={() => openProposal('Fix: ' + c.title, c.path!, () => api.incidentFix(inc.id, i))}>Propose fix</button>
            <button class="btn secondary sm" onclick={() => openProposal('Regression test: ' + c.title, c.path!, () => api.incidentTest(inc.id, i))}>Regression test</button>
          {/if}
          <button class="btn secondary sm" disabled={busy === 'rule'} onclick={() => draftRule(i)}>{busy === 'rule' ? 'Drafting…' : 'Create review rule'}</button>
        </div>
        {#if rule && rule.cause === i}
          <div class="rule">
            <div class="label">New review rule, so this is caught at commit time next time</div>
            <textarea bind:value={rule.yaml} rows="12" spellcheck="false"></textarea>
            <div class="row">
              <label class="check"><input type="checkbox" bind:checked={rule.team} /> Share with the team (.codist/rules/learned.yaml)</label>
              <span class="spacer"></span>
              <button class="btn secondary sm" onclick={() => (rule = null)}>Cancel</button>
              <button class="btn sm" onclick={saveRule}>Save rule</button>
            </div>
          </div>
        {/if}
      </article>
    {:else}
      <p class="faint">{inc.status === 'analysing' ? 'Waiting for the model…' : 'The model did not name a cause. Use the suspects and code below.'}</p>
    {/each}
  </section>

  <div class="two">
    <section>
      <h2>Suspect commits</h2>
      {#each inc.suspects as s}
        <button class="suspect" onclick={() => goToCommit(s.sha)}>
          <span class="bar" style:width="{Math.round((s.score / maxScore) * 100)}%"></span>
          <span class="s-head"><span class="mono">{short(s.sha)}</span><b>{s.message}</b></span>
          <span class="faint">{s.author} · {ago(s.time)}</span>
          <span class="reasons">{s.reasons.slice(0, 3).join(' · ')}</span>
        </button>
      {:else}
        <p class="faint">No recent commits touched the failing code.</p>
      {/each}
    </section>

    <section>
      <h2>Where in the code</h2>
      {#each inc.locations as l}
        <button class="loc" onclick={() => goToCode(l)}>
          <span class="l-head"><span class="mono">{name(l.path)}:{l.line}</span><span class="faint">{l.method}()</span>{#if l.origin}<span class="tag">thrown here</span>{/if}<span class="faint hits">×{l.hits}</span></span>
          <code class="snip">{l.snippet}</code>
          {#if l.blame.sha}<span class="faint small">last changed in <span class="mono">{short(l.blame.sha)}</span> by {l.blame.author} · {ago(l.blame.time)} — {l.blame.summary}</span>{/if}
          {#each l.issues ?? [] as is}<span class="flag">Codist flagged: {is}</span>{/each}
        </button>
      {:else}
        <p class="faint">None of the stack frames are in this project's code.</p>
      {/each}
    </section>
  </div>

  <section>
    <h2>Errors in the logs</h2>
    {#each inc.errors as e}
      <details class="err">
        <summary><span class="mono ex">{e.exception ? e.exception.slice(e.exception.lastIndexOf('.') + 1) : e.level ?? 'ERROR'}</span><span class="msg">{e.message}</span><span class="faint">×{e.count}</span></summary>
        {#if e.firstTime}<div class="faint small">{e.firstTime}{e.lastTime && e.lastTime !== e.firstTime ? ' → ' + e.lastTime : ''}</div>{/if}
        {#each e.causedBy ?? [] as cb}<div class="mono small">Caused by: {cb}</div>{/each}
        <pre class="frames">{e.frames.slice(0, 12).map((f) => `at ${f.class}.${f.method}(${f.file}:${f.line})`).join('\n')}</pre>
      </details>
    {/each}
  </section>

  {#if inc.similar.length}
    <section>
      <h2>Similar past incidents</h2>
      {#each inc.similar as s}
        <button class="similar" onclick={() => openIncident(s.id)}>
          <span><b>{s.title}</b> <span class="faint">· {Math.round(s.score * 100)}% similar · {ago(s.createdAt)}</span></span>
          {#if s.rootCause}<span class="faint">{s.rootCause}</span>{/if}
        </button>
      {/each}
    </section>
  {/if}

  <section>
    <div class="sec-head"><h2>RCA report</h2><span class="spacer"></span><button class="btn secondary sm" onclick={saveReport}>Save</button><button class="btn secondary sm" onclick={() => copy(app.incidentMarkdown, 'RCA report')}>Copy as Markdown</button></div>
    <div class="report">
      {#each sections as [key, label]}
        <label class="label" for="rep-{key}">{label}</label>
        <textarea id="rep-{key}" bind:value={report[key]} rows={key === 'timeline' ? 5 : 3}></textarea>
      {/each}
    </div>
  </section>

  <section>
    <div class="sec-head">
      <h2>Runbook</h2><span class="spacer"></span>
      {#if runbook}
        <button class="btn secondary sm" onclick={saveRunbook}>Save</button>
        <button class="btn secondary sm" onclick={() => copy(runbook, 'Runbook')}>Copy</button>
        {#if app.status?.canFix}<button class="btn secondary sm" onclick={() => openProposal('Runbook: ' + inc.title, 'docs/runbooks', () => api.saveRunbook(inc.id, runbook))}>Add to repository</button>{/if}
      {/if}
      <button class="btn sm" disabled={busy === 'runbook'} onclick={draftRunbook}>{busy === 'runbook' ? 'Drafting…' : runbook ? 'Redraft' : 'Draft runbook'}</button>
    </div>
    {#if runbook}<textarea class="runbook" bind:value={runbook} rows="16" spellcheck="false"></textarea>
    {:else}<p class="faint">A runbook entry for the support team: symptoms, log patterns, diagnosis steps, mitigation and escalation.</p>{/if}
  </section>

  <section>
    <details>
      <summary class="faint">Log excerpt kept with this incident</summary>
      <pre class="excerpt">{inc.excerpt}</pre>
    </details>
  </section>
</div>

<style>
  .detail { max-width: 1080px; margin: 0 auto; padding: 22px 24px 72px; display: grid; gap: 22px; }
  .eyebrow { font-size: 11.5px; color: var(--text-3); }
  h1 { margin: 4px 0 8px; font-size: 20px; font-weight: 600; }
  h2 { margin: 0 0 8px; font-size: 13px; font-weight: 600; color: var(--text-2); text-transform: uppercase; letter-spacing: 0.04em; }
  h3 { margin: 0; font-size: 15px; font-weight: 600; flex: 1; min-width: 0; }
  .status { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 12.5px; color: var(--text-2); }
  .spacer { flex: 1; }
  .pill { padding: 2px 8px; border-radius: 999px; border: 1px solid var(--border-strong); font-size: 12px; }
  .pill.ok { color: var(--good); border-color: color-mix(in srgb, var(--good) 45%, transparent); }
  .pill.bad { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 45%, transparent); }
  .warn { margin: 8px 0 0; color: var(--warn); font-size: 12.5px; }

  .cause { padding: 14px 16px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface); display: grid; gap: 8px; }
  .cause + .cause { margin-top: 10px; }
  .cause.confirmed { border-color: color-mix(in srgb, var(--good) 55%, transparent); box-shadow: inset 3px 0 0 var(--good); }
  .cause-head { display: flex; align-items: center; gap: 10px; }
  .cause p { margin: 0; color: var(--text-2); line-height: 1.6; }
  .conf { font-size: 11px; font-weight: 600; padding: 1px 7px; border-radius: 4px; white-space: nowrap; }
  .conf.high { color: var(--good); background: color-mix(in srgb, var(--good) 13%, transparent); }
  .conf.medium { color: var(--sev-medium); background: color-mix(in srgb, var(--sev-medium) 13%, transparent); }
  .conf.low { color: var(--text-2); background: var(--surface-3); }
  .ref { padding: 1px 6px; border: 0; border-radius: 4px; background: var(--surface-3); color: var(--accent-text); font-size: 11.5px; }
  .evidence { list-style: none; margin: 0; padding: 0; display: grid; gap: 4px; }
  .evidence li { display: flex; align-items: baseline; gap: 8px; min-width: 0; font-size: 12px; }
  .ev { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0; font-size: 11.5px; }
  .kind { flex-shrink: 0; width: 52px; text-align: center; font-size: 10.5px; font-weight: 600; padding: 0 4px; border-radius: 3px; background: var(--surface-3); color: var(--text-2); }
  .kind.log { color: var(--sev-high); }
  .kind.code { color: var(--accent-text); }
  .kind.commit { color: var(--sev-medium); }
  .fixline { font-size: 13px; }
  .fixline b { color: var(--text); }
  .row { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .btn:disabled { opacity: 0.5; cursor: default; filter: none; }
  .rule { display: grid; gap: 6px; padding-top: 6px; border-top: 1px solid var(--border); }
  .check { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-2); }

  .two { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 18px; }
  .suspect, .loc, .similar { position: relative; display: grid; gap: 2px; width: 100%; padding: 9px 10px; margin-bottom: 6px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface); text-align: left; overflow: hidden; }
  .suspect:hover, .loc:hover, .similar:hover { border-color: var(--border-strong); }
  .bar { position: absolute; left: 0; top: 0; bottom: 0; background: var(--accent-soft); z-index: 0; }
  .suspect > :not(.bar) { position: relative; }
  .s-head { display: flex; gap: 8px; align-items: baseline; min-width: 0; }
  .s-head b { font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .s-head .mono { color: var(--accent-text); font-size: 11.5px; }
  .reasons { font-size: 11.5px; color: var(--text-2); }
  .l-head { display: flex; gap: 8px; align-items: baseline; }
  .l-head .mono { color: var(--accent-text); font-size: 12px; }
  .hits { margin-left: auto; }
  .tag { font-size: 10.5px; padding: 0 5px; border-radius: 3px; border: 1px solid var(--border-strong); color: var(--sev-high); }
  .snip { font-size: 11.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text); }
  .small { font-size: 11.5px; }
  .flag { font-size: 11.5px; color: var(--sev-critical); }

  .err { border-bottom: 1px solid var(--border); padding: 6px 0; }
  .err summary { display: flex; gap: 8px; align-items: baseline; cursor: pointer; min-width: 0; }
  .ex { color: var(--sev-high); font-size: 12px; flex-shrink: 0; }
  .msg { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12.5px; }
  .frames, .excerpt { margin: 6px 0 0; padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--r); background: var(--code-bg); font: 11.5px/1.5 var(--mono); overflow: auto; max-height: 260px; }
  .excerpt { max-height: 420px; }
  .sec-head { display: flex; align-items: center; gap: 6px; margin-bottom: 8px; }
  .sec-head h2 { margin: 0; }
  .report { display: grid; gap: 4px; }
  .report textarea, .rule textarea, .runbook { width: 100%; padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface); color: var(--text); font: 13px/1.55 var(--font); resize: vertical; }
  .rule textarea, .runbook { font: 12px/1.55 var(--mono); }
  .report textarea:focus, .rule textarea:focus, .runbook:focus { outline: none; border-color: var(--accent); }
  .report .label { margin-top: 6px; }
  @media (max-width: 900px) { .two { grid-template-columns: 1fr; } }
</style>
