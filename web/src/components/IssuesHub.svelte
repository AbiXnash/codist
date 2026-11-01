<script lang="ts">
  import { app, issueAction, openFile, pickIssue, proposeFix, selectCommit, showWorking } from '../lib/store.svelte';
  import { ago, INSTANT_REFS, NOT_FIXABLE, SEVERITIES } from '../lib/theme';
  import CommitRef from './CommitRef.svelte';
  import CopyButton from './CopyButton.svelte';
  import FixSuggest from './FixSuggest.svelte';
  import type { FindingWithPath, FixRef, Issue, Severity } from '../lib/types';

  let filtersOpen = $state(false);
  const activeFilters = $derived((app.scope !== 'all' ? 1 : 0) + [app.stage, app.kind, app.urgency, app.change].filter(Boolean).length);
  function clearFilters() {
    setScope('all');
    app.stage = app.kind = app.urgency = app.change = null;
  }
  let closing = $state(false);
  let reason = $state('');
  const LEVELS: Severity[] = ['critical', 'high', 'medium', 'low', 'info'];
  const states = [
    { id: 'open', label: 'Open' },
    { id: 'resolved', label: 'Fixed' },
    { id: 'ignored', label: 'Ignored' },
  ] as const;

  // Level counts follow the scope and state, so the meter always describes the list below it.
  const perLevel = $derived.by(() => {
    const m: Partial<Record<Severity, number>> = {};
    for (const g of app.scoped) m[g[0].severity] = (m[g[0].severity] ?? 0) + 1;
    return m;
  });
  const stateCount = (s: Issue['state']) => app.groupsIn(s).length;
  const tally = (f: (i: Issue) => string | undefined) => {
    const c = new Map<string, number>();
    for (const g of app.scoped) { const v = f(g[0]); if (v) c.set(v, (c.get(v) ?? 0) + 1); }
    return [...c].sort((a, b) => b[1] - a[1]);
  };
  const kinds = $derived(tally((i) => i.kind));
  const urgencies = $derived(tally((i) => i.urgency));
  const changes = $derived(tally((i) => i.change));
  const sections = $derived(
    SEVERITIES.map((s) => ({ level: s, groups: app.hub.filter((g) => g[0].severity === s) })).filter((x) => x.groups.length),
  );
  let collapsed = $state<Record<string, boolean>>({});

  const name = (p: string) => p.slice(p.lastIndexOf('/') + 1);
  const short = (s?: string) => (s ? s.slice(0, 7) : '');
  const who = (n?: string) => (app.isMe(n) ? 'you' : n || 'unknown');
  const asFinding = (i: Issue): FindingWithPath =>
    ({ line: i.reviewLine, endLine: i.reviewLine, severity: i.severity, category: i.category, title: i.title, detail: i.detail,
       suggestion: i.suggestion, nodeId: '', onChangedLine: true, source: i.source, ref: i.ref, snippet: i.snippet, path: i.path }) as FindingWithPath;

  function setScope(s: typeof app.scope) {
    app.scope = s;
    app.picked = null;
  }
  function setState(s: typeof app.issueState) {
    app.issueState = s;
    app.picked = null;
  }
  const others = (g: Issue[]): FixRef[] => g.slice(1).map((m) => ({ sha: m.lastSeen, path: m.path, line: m.reviewLine, title: m.title }));
  const isWorking = (i: Issue) => i.firstSeen === 'working';
  const changedFiles = $derived(app.working?.changes.length ?? 0);
  function openCommit(sha: string) {
    void selectCommit(sha, true);
    app.tab = 'review';
  }
</script>

<div class="hub">
  {#if app.pickedIssue}
    {@const g = app.pickedIssue}
    {@const i = g[0]}
    <div class="detail">
      <button class="back" onclick={() => (app.picked = null)}>
        <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M10 3 5 8l5 5"/></svg>
        All issues
      </button>
      <div class="dhead">
        <span class="sev {i.severity}" class:fx={i.state === 'resolved'}>{i.severity}</span>
        {#if i.kind}<span class="tg k">{i.kind}</span>{/if}
        {#if i.urgency}<span class="tg u {i.urgency === 'Hotfix' ? 'hot' : ''}">{i.urgency}</span>{/if}
        {#if i.change}<span class="tg">{i.change} change</span>{/if}
        <CopyButton title="Copy this issue" text={() => [`[${i.severity}] ${i.title}`, ...g.map((m) => `${m.path}${m.line ? ':' + m.line : ''}`), i.detail, i.suggestion ? `Recommendation: ${i.suggestion}` : ''].filter(Boolean).join('\n')} />
        {#if i.state === 'resolved'}<span class="tag good">✓ fixed</span>{:else if i.state === 'ignored'}<span class="tag">ignored</span>{/if}
        {#if i.reopenedIn}<span class="tag bad" title="It was fixed, then the code came back">reopened</span>{/if}
        {#if i.ref && INSTANT_REFS.has(i.ref) && i.state === 'open'}<span class="tag accent" title="Mechanical fix: no model needed">⚡ instant fix</span>{/if}
      </div>
      <h3>{i.title}</h3>
      {#if isWorking(i)}<p class="note">In your uncommitted changes. Fix it before you commit: the fix goes into your working tree, nothing is committed.</p>{/if}
      {#if i.detail}<p class="muted">{i.detail}</p>{/if}

      <div class="block">
        <div class="label">{g.length > 1 ? `${g.length} places, one root cause` : 'Where'}</div>
        {#each g as m (m.id)}
          <button class="place" disabled={m.state !== 'open'} onclick={() => openFile(m.path, null, m.line)}>
            <span class="mono loc">{name(m.path)}{m.line ? ':' + m.line : ''}</span>
            <code>{m.snippet}</code>
          </button>
        {/each}
      </div>

      {#if i.suggestion}
        <div class="block">
          <div class="label">Suggested fix</div>
          <pre class="sugg">{i.suggestion}</pre>
        </div>
      {/if}

      {#if !isWorking(i)}
      <div class="block">
        <div class="label">History</div>
        <ol class="tl">
          <li><span class="dot intro"></span>Introduced in <CommitRef sha={i.firstSeen} /> by <b>{who(i.firstBy)}</b> · {ago(i.firstTime)}<div class="tmsg">{i.firstMsg}</div></li>
          {#if i.commits.length > 1}<li><span class="dot seen"></span>Still reported in {i.commits.length - 1} later review{i.commits.length > 2 ? 's' : ''}</li>{/if}
          {#if i.reopenedIn}
            <li><span class="dot done"></span>Fixed in <CommitRef sha={i.wasFixedIn ?? ''} /></li>
            <li><span class="dot intro"></span>Brought back in <CommitRef sha={i.reopenedIn} /> by <b>{who(i.reopenedBy)}</b> · {ago(i.reopenedAt ?? 0)}<div class="tmsg">{i.reopenedMsg}</div></li>
          {/if}
          {#if i.state === 'resolved'}
            <li><span class="dot done"></span>
              {#if i.manual}Marked as fixed by <b>{who(i.fixedBy)}</b> · {ago(i.fixedAt ?? 0)}
              {:else if i.fixedIn}Fixed in <CommitRef sha={i.fixedIn} /> by <b>{who(i.fixedBy)}</b> · {ago(i.fixedAt ?? 0)}<div class="tmsg">{i.fixedMsg}</div>
              {:else if i.uncommitted}Fixed in the working tree, not committed yet{:else}Fixed{/if}
            </li>
          {:else if i.state === 'ignored'}<li><span class="dot seen"></span>Ignored</li>
          {:else}<li><span class="dot open"></span>Still open</li>{/if}
        </ol>
      </div>
      {/if}

      {#if app.featureOn('issueWorkflow')}<div class="flow" aria-label="Workflow">
        {#each ['Open', 'In progress', 'In review', 'Closed'] as st, n}
          {@const at = i.state !== 'open' ? 3 : i.stage === 'in-review' ? 2 : i.stage === 'in-progress' ? 1 : 0}
          <span class="st" class:done={n < at} class:now={n === at}>{n === 3 && i.state === 'ignored' ? "Won't fix" : n === 3 && i.state === 'resolved' ? 'Fixed' : st}</span>
        {/each}
        {#if i.assignee}<span class="who">· {who(i.assignee)}</span>{/if}
      </div>{/if}
      {#if !isWorking(i)}
        <div class="actions">
          {#if i.state === 'open'}
            {#if !i.stage && app.featureOn('issueWorkflow')}<button class="btn secondary" title="Assign it to yourself" onclick={() => issueAction(g, 'start')}>Start</button>{/if}
            {#if i.stage === 'in-progress' && app.featureOn('issueWorkflow')}<button class="btn secondary" onclick={() => issueAction(g, 'review')}>Send to review</button>{/if}
            {#if i.stage && app.featureOn('issueWorkflow')}<button class="btn secondary" onclick={() => issueAction(g, 'stop')}>Back to open</button>{/if}
            <button class="btn secondary" title="You already fixed it: close it. It reopens if a later review reports it again." onclick={() => issueAction(g, 'fixed')}>Mark fixed</button>
            <button class="btn secondary" onclick={() => { closing = !closing; }}>Won't fix…</button>
          {:else if i.manual || i.ignoreId}
            <button class="btn secondary" onclick={() => issueAction(g, 'reopen')}>Reopen</button>
          {/if}
        </div>
        {#if closing && i.state === 'open'}
          <div class="close">
            <textarea rows="2" bind:value={reason} placeholder="Why not? Optional. Codist remembers this in later reviews."></textarea>
            <button class="btn sm" onclick={() => { closing = false; void issueAction(g, 'wontfix', reason); reason = ''; }}>Close as won't fix</button>
          </div>
        {/if}
      {/if}

      <div class="actions">
        {#if i.state === 'open'}
          {#if NOT_FIXABLE.has(i.ref ?? '')}
            <button class="btn secondary" title="A heads-up, not a defect: confirm the readers still expect the new value" onclick={() => issueAction(g, 'wontfix')}>Mark as checked</button>
          {:else}
            {#if app.status?.canFix && g.every((m) => m.path.endsWith('.java'))}
              <button class="btn" onclick={() => proposeFix(asFinding(i), i.lastSeen, others(g))}>{i.ref && INSTANT_REFS.has(i.ref) ? '⚡ Fix' : 'Fix with AI'}{g.length > 1 ? ` all ${g.length} places` : ''}</button>
            {/if}
            {/if}
        {/if}
        <button class="btn secondary" onclick={() => openFile(i.path, null, i.line || null)}>Open code</button>
        {#if !isWorking(i)}<button class="btn secondary" onclick={() => openCommit(i.firstSeen)}>Open commit</button>{/if}
        {#if i.ref}<span class="faint ref mono">{i.ref}</span>{/if}
      </div>
      {#if i.state === 'open' && !NOT_FIXABLE.has(i.ref ?? '') && g.every((m) => m.path.endsWith('.java'))}
        <FixSuggest finding={asFinding(i)} sha={i.lastSeen} also={others(g)} />
      {/if}
    </div>
  {:else}
    <div class="top">
      <div class="row2">
        <div class="seg states">
          {#each states as s}
            <button class:on={app.issueState === s.id} onclick={() => setState(s.id)}>{s.label} <span class="n">{stateCount(s.id)}</span></button>
          {/each}
        </div>
        <input class="q" bind:value={app.hubQuery} placeholder="Search issues, files, rules…" spellcheck="false" />
      </div>
      <div class="row3">
        <button class="fbtn" class:on={filtersOpen || activeFilters > 0} aria-expanded={filtersOpen} onclick={() => (filtersOpen = !filtersOpen)}>
          <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M2 4h12M4.5 8h7M7 12h2"/></svg>
          Filter{#if activeFilters} ({activeFilters}){/if}
        </button>
        {#if app.scope !== 'all'}<span class="scoped">{app.scope === 'commit' ? 'This commit' : app.scope === 'mine' ? 'Mine' : 'Uncommitted'}</span>{/if}
        {#if activeFilters}<button class="clear" onclick={clearFilters}>Clear</button>{/if}
      </div>
      {#if filtersOpen}
        <div class="filters">
      <div class="scope seg" role="tablist" aria-label="Scope">
        <button class:on={app.scope === 'all'} onclick={() => setScope('all')} title="Every issue in the codebase">All code</button>
        <button class:on={app.scope === 'commit'} disabled={!app.selectedSha} onclick={() => setScope('commit')} title="Introduced or fixed in the selected commit">
          This commit{#if app.selectedSha}<span class="mono faint sha">{app.selectedSha.slice(0, 7)}</span>{/if}
        </button>
        <button class:on={app.scope === 'mine'} disabled={!app.me} onclick={() => setScope('mine')} title="Introduced or fixed by you">Mine</button>
        {#if changedFiles}
          <button class:on={app.scope === 'working'} onclick={showWorking} title="Instant checks on the lines you changed and have not committed">
            Uncommitted{#if app.working?.findings}<span class="dotn">{app.working.findings}</span>{/if}
          </button>
        {/if}
      </div>
      <div class="triage">
        <select aria-label="Workflow" value={app.stage ?? ''} class:on={!!app.stage} onchange={(e) => (app.stage = e.currentTarget.value || null)}>
          <option value="">Stage: all</option><option value="unassigned">Unassigned</option><option value="mine">Assigned to me</option><option value="in-progress">In progress</option><option value="in-review">In review</option>
        </select>
        {#each [['kind', 'Type', kinds], ['urgency', 'Urgency', urgencies], ['change', 'Change', changes]] as [key, title, opts]}
          <select aria-label={title as string} value={app[key as 'kind'] ?? ''} class:on={!!app[key as 'kind']} onchange={(e) => (app[key as 'kind'] = e.currentTarget.value || null)}>
            <option value="">{title}: all</option>
            {#each opts as [v, n]}<option value={v}>{v} ({n})</option>{/each}
          </select>
        {/each}
      </div>
        </div>
      {/if}
      <div class="levels">
        {#each LEVELS as l}
          {@const n = perLevel[l] ?? 0}
          <button class="level {l}" class:fx={app.issueState === 'resolved'} class:on={app.level === l} class:off={app.level && app.level !== l} class:zero={!n}
            onclick={() => (app.level = app.level === l ? null : l)} title="Show only {l}">
            <b>{n}</b><span>{l}</span>
          </button>
        {/each}
      </div>

    </div>

    <div class="list">
      {#each sections as sec (sec.level)}
        <section>
          <button class="shead" onclick={() => (collapsed[sec.level] = !collapsed[sec.level])} aria-expanded={!collapsed[sec.level]}>
            <svg class="chev" class:closed={collapsed[sec.level]} width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2"><path d="m4 6 4 4 4-4"/></svg>
            <span class="sev {sec.level}" class:fx={app.issueState === 'resolved'}>{sec.level}</span>
            <span class="faint">{sec.groups.length}</span>
          </button>
          {#if !collapsed[sec.level]}
            {#each sec.groups as g (g[0].id)}
              {@const i = g[0]}
              <button class="issue" class:fx={i.state === 'resolved'} onclick={() => pickIssue(i.id)}>
                <span class="bar {i.severity}" class:fx={i.state === 'resolved'}></span>
                <span class="body">
                  <span class="title">{i.title}</span>
                  <span class="tags">
                    {#if i.kind}<span class="tg k">{i.kind}</span>{/if}
                    {#if i.urgency}<span class="tg u {i.urgency === 'Hotfix' ? 'hot' : ''}">{i.urgency}</span>{/if}
                    {#if i.change === 'Major'}<span class="tg">Major</span>{/if}
                    {#if i.stage}<span class="tg k">{i.stage === 'in-review' ? 'In review' : 'In progress'}{i.assignee ? ` · ${who(i.assignee)}` : ''}</span>{/if}
                  </span>
                  <span class="sub">
                    <span class="mono loc">{g.length > 1 ? `${g.length} places` : `${name(i.path)}${i.line ? ':' + i.line : ''}`}</span>
                    {#if i.state === 'resolved'}
                      <span>✓ {i.fixedIn ? `${short(i.fixedIn)} by ${who(i.fixedBy)}` : i.uncommitted ? 'fixed locally' : 'fixed'}</span>
                    {:else}
                      {#if isWorking(i)}<span class="uncommitted">uncommitted</span>
                      {:else}<span><span class="mono">{short(i.firstSeen)}</span> · {who(i.firstBy)} · {ago(i.firstTime)}</span>{/if}
                    {/if}
                  </span>
                </span>
                {#if i.state === 'open' && i.ref && INSTANT_REFS.has(i.ref)}<span class="zap" title="Instant fix">⚡</span>{/if}
                {#if i.reopenedIn}<span class="src re" title="Fixed in {short(i.wasFixedIn)}, brought back in {short(i.reopenedIn)} by {who(i.reopenedBy)}">reopened</span>{/if}
                {#if i.source === 'llm'}<span class="src" title="Found by the AI model">AI</span>{/if}
              </button>
            {/each}
          {/if}
        </section>
      {:else}
        <div class="empty">
          {#if app.issueState === 'open'}
            <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="var(--good)" stroke-width="1.8"><circle cx="12" cy="12" r="9"/><path d="m8 12.5 2.5 2.5L16 9.5"/></svg>
            <span>No open issues {app.scope === 'commit' ? 'from this commit' : app.scope === 'mine' ? 'by you' : app.scope === 'working' ? 'on the lines you changed' : 'in the code'}{app.level ? ` at ${app.level}` : ''}.</span>
          {:else}
            <span>No {app.issueState === 'resolved' ? 'fixed' : 'ignored'} issues here.</span>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .hub { display: flex; flex-direction: column; min-height: 100%; }
  .top { position: sticky; top: 0; z-index: 2; display: grid; gap: 10px; padding: 12px 14px; border-bottom: 1px solid var(--border); background: var(--surface); }
  .seg { display: flex; padding: 2px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .seg button { flex: 1; height: 26px; padding: 0 8px; border: 0; border-radius: var(--r); background: none; color: var(--text-3); font-weight: 500; font-size: 12px; white-space: nowrap; }
  .seg button:hover:not(:disabled) { color: var(--text); }
  .seg button.on { color: var(--text); background: var(--surface-3); box-shadow: var(--raised); }
  .seg button:disabled { opacity: 0.4; cursor: default; }
  .seg .mono { font-size: 10.5px; }
  .seg .sha { margin-left: 5px; }
  .dotn { margin-left: 5px; min-width: 16px; padding: 0 4px; border-radius: 8px; font: 600 10px/15px var(--font); color: #fff; background: var(--warn); display: inline-block; }
  .uncommitted { color: var(--warn); }
  .note { margin: 0; padding: 8px 10px; border-radius: var(--r-lg); background: color-mix(in srgb, var(--warn) 10%, transparent); color: var(--text-2); font-size: 12px; }
  .n { font-size: 11px; color: var(--text-3); font-variant-numeric: tabular-nums; }

  .row3 { display: flex; align-items: center; gap: 8px; font-size: 12.5px; }
  .fbtn { display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 10px; border: 0; border-radius: var(--r); background: var(--surface-3); color: var(--text); font-size: 12.5px; }
  .fbtn.on { color: #fff; background: var(--accent); }
  .scoped { color: var(--text-2); }
  .clear { border: 0; background: none; padding: 0; color: var(--accent-text); font-size: 12.5px; }
  .filters { display: grid; gap: 8px; padding: 10px; border-radius: var(--r-lg); background: var(--surface-2); }
  .levels { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 5px; }
  .level { --c: var(--sev-low); position: relative; overflow: hidden; display: grid; gap: 0; padding: 6px 8px 5px 10px; border: 1px solid transparent; border-radius: var(--r-lg); background: var(--surface-2); text-align: left; overflow: hidden; transition: border-color 0.15s, opacity 0.15s; }
  .level::before { content: ''; position: absolute; left: 0; top: 0; bottom: 0; width: 3px; background: var(--c); }
  .level b { font-size: 20px; line-height: 1.15; font-weight: 600; font-variant-numeric: tabular-nums; color: var(--c); }
  .level span { font-size: 11px; color: var(--text-3); text-transform: capitalize; }
  /* Fixed: one green, strong to light by level. */
  .level.level.fx, .bar.bar.fx { --c: var(--good); }
  .level.level.fx b { color: var(--good); }
  .level.fx::before { opacity: var(--tone); }
  .bar.fx { opacity: var(--tone); }
  .level.fx.critical, .bar.fx.critical { --tone: 1; }
  .level.fx.high, .bar.fx.high { --tone: 0.75; }
  .level.fx.medium, .bar.fx.medium { --tone: 0.5; }
  .level.fx.low, .bar.fx.low { --tone: 0.3; }
  .level.fx.info, .bar.fx.info { --tone: 0.16; }
  .issue.fx .tg, .issue.fx .tg.hot { color: var(--good); background: color-mix(in srgb, var(--good) 12%, transparent); border-color: transparent; }
  .issue.fx .sub { color: var(--good); }
  .level:hover { border-color: var(--border-strong); }
  .level.on { border-color: var(--c); background: color-mix(in srgb, var(--c) 9%, var(--surface-2)); }
  .level.off { opacity: 0.45; }
  .level.zero b { color: var(--text-3); }
  .level.critical { --c: var(--sev-critical); }
  .level.high { --c: var(--sev-high); }
  .level.medium { --c: var(--sev-medium); }
  .level.info { --c: var(--sev-info); }
  :global(:root[data-theme='dark']) .level.low { --c: #94a3b8; }

  .triage { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 6px; }
  .flow { display: flex; flex-wrap: wrap; align-items: center; gap: 3px; font-size: 11.5px; }
  .flow .st { padding: 2px 9px; border-radius: 10px; color: var(--text-3); background: var(--surface-2); border: 1px solid var(--border); }
  .flow .st.done { color: var(--good); }
  .flow .st.now { color: var(--text); font-weight: 600; border-color: var(--accent); background: var(--accent-soft); }
  .flow .who { margin-left: 4px; color: var(--text-2); }
  .close { display: grid; gap: 6px; }
  .close textarea { padding: 7px 9px; border: 1px solid var(--border-strong); border-radius: var(--r); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12.5px; resize: vertical; }
  .close .btn { justify-self: start; }
  .triage select { min-width: 0; height: 26px; padding: 0 5px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); color: var(--text-2); font: inherit; font-size: 11.5px; }
  .triage select.on { border-color: var(--accent); color: var(--text); }
  .tags { display: flex; flex-wrap: wrap; gap: 3px; }
  .tg { font: 600 10px var(--font); padding: 0 6px; line-height: 16px; border-radius: 8px; background: var(--surface-3); color: var(--text-2); white-space: nowrap; }
  .tg.k { color: var(--accent-text); background: var(--accent-soft); }
  .tg.u { color: var(--text); }
  .tg.u.hot { color: #fff; background: var(--sev-critical); }
  .row2 { display: flex; gap: 8px; flex-wrap: wrap; }
  .states { flex: 0 0 auto; }
  .q { flex: 1; min-width: 140px; height: 30px; padding: 0 10px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12.5px; }
  .q:focus { outline: none; border-color: var(--accent); }

  .list { padding: 6px 8px 20px; }
  .shead { display: flex; align-items: center; gap: 8px; width: 100%; padding: 8px 6px 4px; border: 0; background: none; color: var(--text-3); font-size: 11.5px; }
  .chev { transition: transform 0.15s; }
  .chev.closed { transform: rotate(-90deg); }
  .issue { display: flex; align-items: center; gap: 10px; width: 100%; padding: 8px 8px 8px 0; border: 0; border-radius: var(--r-lg); background: none; text-align: left; }
  .issue:hover { background: var(--hover); }
  .bar { --c: var(--sev-low); align-self: stretch; width: 3px; border-radius: 2px; background: var(--c); margin-left: 4px; flex-shrink: 0; }
  .bar.critical { --c: var(--sev-critical); } .bar.high { --c: var(--sev-high); } .bar.medium { --c: var(--sev-medium); }
  .body { flex: 1; min-width: 0; display: grid; gap: 2px; }
  .title { font-size: 12.5px; line-height: 1.4; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; }
  .sub { display: flex; gap: 8px; font-size: 11px; color: var(--text-3); min-width: 0; white-space: nowrap; overflow: hidden; }
  .sub .mono { font-size: 10.5px; }
  .loc { color: var(--accent-text); }
  .zap { font-size: 12px; }
  .src { font: 600 9.5px var(--font); padding: 1px 5px; border-radius: 4px; color: var(--text-2); border: 1px solid var(--border-strong); }
  .empty { display: grid; justify-items: center; gap: 10px; padding: 56px 20px; color: var(--text-2); text-align: center; }

  .detail { padding: 12px 16px 24px; display: grid; gap: 12px; }
  .back { justify-self: start; display: inline-flex; align-items: center; gap: 4px; height: 26px; padding: 0 8px 0 4px; border: 0; border-radius: var(--r); background: none; color: var(--text-2); font-size: 12px; }
  .back:hover { background: var(--hover); color: var(--text); }
  .dhead { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .cat { font-size: 11.5px; color: var(--text-3); text-transform: capitalize; }
  .tag { font: 600 10.5px var(--font); padding: 1px 7px; border-radius: 9px; background: var(--surface-3); color: var(--text-2); }
  .tag.bad { color: var(--bad); background: color-mix(in srgb, var(--bad) 13%, transparent); }
  .src.re { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 45%, transparent); }
  .tag.good { color: var(--good); background: color-mix(in srgb, var(--good) 13%, transparent); }
  .tag.accent { color: var(--accent-text); background: var(--accent-soft); }
  h3 { margin: 0; font-size: 15px; line-height: 1.4; font-weight: 600; }
  .detail p { margin: 0; line-height: 1.55; }
  .block { display: grid; gap: 6px; }
  .place { display: grid; gap: 2px; padding: 7px 10px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); text-align: left; }
  .place:hover:not(:disabled) { border-color: var(--accent); }
  .place:disabled { cursor: default; opacity: 0.7; }
  .place .loc { font-size: 11px; }
  .place code { font-size: 11.5px; color: var(--text-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sugg { margin: 0; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--code-bg); font-size: 11.5px; line-height: 1.55; white-space: pre-wrap; overflow-wrap: anywhere; }
  .tl { list-style: none; margin: 0; padding: 0 0 0 4px; font-size: 12px; color: var(--text-2); }
  .tl li { position: relative; padding: 0 0 8px 16px; border-left: 1px solid var(--border-strong); margin-left: 3px; }
  .tl li:last-child { border-left-color: transparent; }
  .tl b { color: var(--text); font-weight: 500; }
  .tl .dot { position: absolute; left: -4px; top: 5px; width: 7px; height: 7px; border-radius: 50%; }
  .dot.intro { background: var(--sev-high); } .dot.seen { background: var(--text-3); } .dot.done { background: var(--good); } .dot.open { background: var(--sev-medium); }
  .tmsg { color: var(--text-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .actions { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; padding-top: 4px; }
  .ref { margin-left: auto; font-size: 11px; }
</style>
