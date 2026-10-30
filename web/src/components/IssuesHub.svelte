<script lang="ts">
  import { app, ignoreFinding, loadIssues, loadWorking, openFile, pickIssue, proposeFix, selectCommit, showWorking, unignore } from '../lib/store.svelte';
  import { ago, INSTANT_REFS, NOT_FIXABLE, SEVERITIES } from '../lib/theme';
  import CommitRef from './CommitRef.svelte';
  import FixSuggest from './FixSuggest.svelte';
  import type { FindingWithPath, Issue, Severity } from '../lib/types';

  const LEVELS: Severity[] = ['critical', 'high', 'medium', 'low'];
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
  const stateCount = (s: string) => {
    if (app.scope === 'working') return app.workingIssues.filter((i) => i.state === s && (!i.group || i.groupRoot)).length;
    const all = app.issueList?.issues ?? [];
    const sha = app.selectedSha;
    return all.filter((i) => i.state === s && (s !== 'open' || !i.group || i.groupRoot) &&
      (app.scope === 'all' || (app.scope === 'mine' ? app.isMe(i.firstBy) || app.isMe(i.fixedBy) : i.firstSeen === sha || i.fixedIn === sha))).length;
  };
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
  async function ignore(g: Issue[]) {
    const prev = app.selectedSha;
    for (const i of g) {
      app.selectedSha = i.lastSeen;
      await ignoreFinding(asFinding(i), 'code');
    }
    app.selectedSha = prev;
    app.picked = null;
    void loadIssues();
    void loadWorking();
  }
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
        <span class="sev {i.severity}">{i.severity}</span>
        <span class="cat">{i.category}</span>
        {#if i.state === 'resolved'}<span class="tag good">✓ fixed</span>{:else if i.state === 'ignored'}<span class="tag">ignored</span>{/if}
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
          {#if i.state === 'resolved'}
            <li><span class="dot done"></span>
              {#if i.fixedIn}Fixed in <CommitRef sha={i.fixedIn} /> by <b>{who(i.fixedBy)}</b> · {ago(i.fixedAt ?? 0)}<div class="tmsg">{i.fixedMsg}</div>
              {:else if i.uncommitted}Fixed in the working tree, not committed yet{:else}Fixed{/if}
            </li>
          {:else if i.state === 'ignored'}<li><span class="dot seen"></span>Ignored</li>
          {:else}<li><span class="dot open"></span>Still open</li>{/if}
        </ol>
      </div>
      {/if}

      <div class="actions">
        {#if i.state === 'open'}
          {#if NOT_FIXABLE.has(i.ref ?? '')}
            <button class="btn secondary" title="A heads-up, not a defect: confirm the readers still expect the new value" onclick={() => ignore(g)}>Mark as checked</button>
          {:else}
            {#if app.status?.canFix && g.every((m) => m.path.endsWith('.java'))}
              <button class="btn" onclick={() => proposeFix(asFinding(i), i.lastSeen)}>{i.ref && INSTANT_REFS.has(i.ref) ? '⚡ Fix' : 'Fix with AI'}</button>
            {/if}
            <button class="btn secondary" onclick={() => ignore(g)}>Ignore</button>
          {/if}
        {:else if i.state === 'ignored' && i.ignoreId}
          <button class="btn secondary" onclick={() => unignore(i.ignoreId!).then(loadIssues)}>Stop ignoring</button>
        {/if}
        <button class="btn secondary" onclick={() => openFile(i.path, null, i.line || null)}>Open code</button>
        {#if !isWorking(i)}<button class="btn secondary" onclick={() => openCommit(i.firstSeen)}>Open commit</button>{/if}
        {#if i.ref}<span class="faint ref mono">{i.ref}</span>{/if}
      </div>
      {#if i.state === 'open' && !NOT_FIXABLE.has(i.ref ?? '') && g.every((m) => m.path.endsWith('.java'))}
        <FixSuggest finding={asFinding(i)} sha={i.lastSeen} />
      {/if}
    </div>
  {:else}
    <div class="top">
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

      <div class="levels">
        {#each LEVELS as l}
          {@const n = perLevel[l] ?? 0}
          <button class="level {l}" class:on={app.level === l} class:off={app.level && app.level !== l} class:zero={!n}
            onclick={() => (app.level = app.level === l ? null : l)} title="Show only {l}">
            <b>{n}</b><span>{l}</span>
          </button>
        {/each}
      </div>

      <div class="row2">
        <div class="seg states">
          {#each states as s}
            <button class:on={app.issueState === s.id} onclick={() => setState(s.id)}>{s.label} <span class="n">{stateCount(s.id)}</span></button>
          {/each}
        </div>
        <input class="q" bind:value={app.hubQuery} placeholder="Search issues, files, rules…" spellcheck="false" />
      </div>
    </div>

    <div class="list">
      {#each sections as sec (sec.level)}
        <section>
          <button class="shead" onclick={() => (collapsed[sec.level] = !collapsed[sec.level])} aria-expanded={!collapsed[sec.level]}>
            <svg class="chev" class:closed={collapsed[sec.level]} width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2"><path d="m4 6 4 4 4-4"/></svg>
            <span class="sev {sec.level}">{sec.level}</span>
            <span class="faint">{sec.groups.length}</span>
          </button>
          {#if !collapsed[sec.level]}
            {#each sec.groups as g (g[0].id)}
              {@const i = g[0]}
              <button class="issue" onclick={() => pickIssue(i.id)}>
                <span class="bar {i.severity}"></span>
                <span class="body">
                  <span class="title">{i.title}</span>
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
  .seg button.on { color: var(--text); background: var(--surface-3); box-shadow: 0 0 0 1px var(--border-strong); }
  .seg button:disabled { opacity: 0.4; cursor: default; }
  .seg .mono { font-size: 10.5px; }
  .seg .sha { margin-left: 5px; }
  .dotn { margin-left: 5px; min-width: 16px; padding: 0 4px; border-radius: 8px; font: 600 10px/15px var(--font); color: #fff; background: var(--warn); display: inline-block; }
  .uncommitted { color: var(--warn); }
  .note { margin: 0; padding: 8px 10px; border-radius: var(--r-lg); background: color-mix(in srgb, var(--warn) 10%, transparent); color: var(--text-2); font-size: 12px; }
  .n { font-size: 11px; color: var(--text-3); font-variant-numeric: tabular-nums; }

  .levels { display: grid; grid-template-columns: repeat(4, 1fr); gap: 6px; }
  .level { --c: var(--sev-low); position: relative; display: grid; gap: 0; padding: 8px 10px 7px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); text-align: left; overflow: hidden; transition: border-color 0.15s, opacity 0.15s; }
  .level::before { content: ''; position: absolute; left: 0; top: 0; bottom: 0; width: 3px; background: var(--c); }
  .level b { font-size: 20px; line-height: 1.15; font-weight: 600; font-variant-numeric: tabular-nums; color: var(--c); }
  .level span { font-size: 11px; color: var(--text-3); text-transform: capitalize; }
  .level:hover { border-color: var(--border-strong); }
  .level.on { border-color: var(--c); background: color-mix(in srgb, var(--c) 9%, var(--surface-2)); }
  .level.off { opacity: 0.45; }
  .level.zero b { color: var(--text-3); }
  .level.critical { --c: var(--sev-critical); }
  .level.high { --c: var(--sev-high); }
  .level.medium { --c: var(--sev-medium); }
  :global(:root[data-theme='dark']) .level.low { --c: #94a3b8; }

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
