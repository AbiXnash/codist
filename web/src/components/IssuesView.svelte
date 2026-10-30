<script lang="ts">
  import { app, ignoreFinding, loadIssues, openFile, proposeFix, unignore } from '../lib/store.svelte';
  import { ago, SEVERITIES } from '../lib/theme';
  import CommitRef from './CommitRef.svelte';
  import type { FindingWithPath, Issue, Severity } from '../lib/types';

  let query = $state('');
  let severity = $state<Severity | null>(null);
  let expanded = $state<string | null>(null);

  const states = ['open', 'resolved', 'ignored'] as const;
  const list = $derived(app.issueList?.issues ?? []);
  const filtered = $derived(
    list.filter((i) => (!severity || i.severity === severity) && (!query || (i.title + ' ' + i.path).toLowerCase().includes(query.toLowerCase()))),
  );
  // One row per group of open issues (same root cause), root first.
  const groups = $derived.by(() => {
    const m = new Map<string, Issue[]>();
    filtered.forEach((i) => {
      const k = i.state === 'open' && i.group ? i.group : i.id;
      m.set(k, [...(m.get(k) ?? []), i]);
    });
    return [...m.values()].map((g) => [...g].sort((a, b) => Number(!!b.groupRoot) - Number(!!a.groupRoot)));
  });

  const name = (p: string) => p.slice(p.lastIndexOf('/') + 1);
  const short = (s?: string) => (s ? s.slice(0, 7) : '');
  const asFinding = (i: Issue): FindingWithPath =>
    ({ line: i.reviewLine, endLine: i.reviewLine, severity: i.severity, category: i.category, title: i.title, detail: i.detail,
       suggestion: i.suggestion, nodeId: '', onChangedLine: true, source: i.source, ref: i.ref, snippet: i.snippet, path: i.path }) as FindingWithPath;

  function setState(s: (typeof states)[number]) {
    app.issueState = s;
    expanded = null;
    void loadIssues();
  }
  function fix(g: Issue[]) {
    void proposeFix(asFinding(g[0]), g[0].lastSeen);
  }
  async function ignore(g: Issue[]) {
    const prev = app.selectedSha;
    for (const i of g) {
      app.selectedSha = i.lastSeen;
      await ignoreFinding(asFinding(i), 'code');
    }
    app.selectedSha = prev;
    void loadIssues();
  }
</script>

<div class="issues">
  <div class="summary">
    {#each SEVERITIES as s}
      {@const n = app.issueList?.openBySeverity[s] ?? 0}
      {#if n}<button class="sev {s}" class:off={severity && severity !== s} onclick={() => (severity = severity === s ? null : s)} title="Filter by {s}">{n} {s}</button>{/if}
    {/each}
    {#if !(app.issueList?.counts.open)}<span class="clean">No open issues</span>{/if}
  </div>

  <div class="filters">
    <div class="tabs">
      {#each states as s}
        <button class:on={app.issueState === s} onclick={() => setState(s)}>{s} <span class="faint">{app.issueList?.counts[s] ?? 0}</span></button>
      {/each}
    </div>
    <input class="q" bind:value={query} placeholder="Filter…" spellcheck="false" />
  </div>

  <ul>
    {#each groups as g (g[0].id)}
      {@const i = g[0]}
      <li class:open={expanded === i.id}>
        <button class="row" onclick={() => (expanded = expanded === i.id ? null : i.id)}>
          <span class="sev {i.severity}">{i.severity}</span>
          <span class="title">{i.title}</span>
        </button>
        <div class="sub">
          <span class="loc mono">{g.length > 1 ? `${g.length} places` : `${name(i.path)}${i.line ? ':' + i.line : ''}`}</span> ·
          {#if i.state === 'resolved'}
            {#if i.fixedIn}✓ fixed in <span class="mono">{short(i.fixedIn)}</span> by {i.fixedBy} · {ago(i.fixedAt ?? 0)}{:else if i.uncommitted}✓ fixed locally, not committed{:else}✓ fixed{/if}
          {:else}
            since <span class="mono">{short(i.firstSeen)}</span> · {ago(i.firstTime)}{i.commits.length > 1 ? ` · seen in ${i.commits.length} reviews` : ''}
          {/if}
        </div>
        {#if expanded === i.id}
          <div class="body">
            <ol class="timeline">
              <li><span class="dot intro"></span>Introduced in <CommitRef sha={i.firstSeen} /> by <b>{i.firstBy}</b> · {ago(i.firstTime)}<div class="tmsg">{i.firstMsg}</div></li>
              {#if i.commits.length > 1}<li><span class="dot seen"></span>Still reported in {i.commits.length - 1} later review{i.commits.length > 2 ? 's' : ''}</li>{/if}
              {#if i.state === 'resolved'}
                <li><span class="dot done"></span>
                  {#if i.fixedIn}Fixed in <CommitRef sha={i.fixedIn} /> by <b>{i.fixedBy}</b> · {ago(i.fixedAt ?? 0)}<div class="tmsg">{i.fixedMsg}</div>
                  {:else if i.uncommitted}Fixed in the working tree, not committed yet
                  {:else}Fixed{/if}
                </li>
              {:else if i.state === 'ignored'}
                <li><span class="dot seen"></span>Ignored</li>
              {:else}
                <li><span class="dot open"></span>Open · {name(i.path)}:{i.line}</li>
              {/if}
            </ol>
            {#if i.detail}<p>{i.detail}</p>{/if}
            {#each g as m (m.id)}
              <button class="place mono" disabled={m.state !== 'open'} onclick={() => openFile(m.path, null, m.line)}>{name(m.path)}{m.line ? ':' + m.line : ''}<span class="faint">{m.snippet}</span></button>
            {/each}
            <div class="actions">
              {#if i.state === 'open'}
                {#if app.status?.canFix && g.every((m) => m.path.endsWith('.java'))}<button class="btn sm" onclick={() => fix(g)}>Fix</button>{/if}
                <button class="btn secondary sm" onclick={() => ignore(g)}>Ignore</button>
              {:else if i.state === 'ignored' && i.ignoreId}
                <button class="btn secondary sm" onclick={() => unignore(i.ignoreId!).then(loadIssues)}>Stop ignoring</button>
              {/if}
              {#if i.ref}<span class="faint ref">{i.ref}</span>{/if}
            </div>
          </div>
        {/if}
      </li>
    {:else}
      <li class="empty">{app.issueState === 'open' ? 'Nothing open. 🎉' : `No ${app.issueState} issues.`}</li>
    {/each}
  </ul>
</div>

<style>
  .issues { display: flex; flex-direction: column; min-height: 0; flex: 1; }
  .summary { display: flex; flex-wrap: wrap; gap: 4px; padding: 10px 10px 6px; }
  .summary .sev { cursor: pointer; border: 0; }
  .summary .sev.off { opacity: 0.35; }
  .clean { color: var(--good); font-size: 12px; }
  .filters { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; padding: 0 10px 8px; border-bottom: 1px solid var(--border); }
  .filters .q { flex-basis: 100%; }
  .tabs { display: flex; gap: 2px; }
  .tabs button { padding: 2px 8px; border: 0; border-radius: var(--r); background: none; font-size: 12px; color: var(--text-3); text-transform: capitalize; }
  .tabs button.on { color: var(--text); background: var(--active); }
  .q { flex: 1; min-width: 0; height: 24px; padding: 0 8px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12px; }
  .q:focus { outline: none; border-color: var(--accent); }
  ul { list-style: none; margin: 0; padding: 4px; overflow: auto; flex: 1; }
  li { border-radius: var(--r); padding: 6px 6px 7px; }
  li:hover, li.open { background: var(--hover); }
  .row { display: flex; align-items: flex-start; gap: 6px; width: 100%; padding: 0; border: 0; background: none; text-align: left; }
  .row .sev { margin-top: 1px; flex-shrink: 0; }
  .title { flex: 1; min-width: 0; font-size: 12.5px; line-height: 1.4; display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
  .loc { font-size: 10.5px; color: var(--accent-text); white-space: nowrap; }
  .sub { margin: 2px 0 0 2px; font-size: 11px; color: var(--text-3); }
  .sub .mono { font-size: 10.5px; color: var(--text-2); }
  .body { margin-top: 6px; }
  p { margin: 0 0 6px; font-size: 12px; line-height: 1.5; color: var(--text-2); }
  .place { display: flex; gap: 8px; width: 100%; padding: 3px 6px; border: 0; border-radius: 4px; background: none; text-align: left; font-size: 11px; color: var(--accent-text); }
  .place:hover:not(:disabled) { background: var(--active); }
  .place .faint { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .actions { display: flex; align-items: center; gap: 6px; margin-top: 6px; }
  .timeline { list-style: none; margin: 0 0 8px; padding: 0 0 0 4px; font-size: 11.5px; color: var(--text-2); }
  .timeline li { position: relative; padding: 0 0 6px 14px; border-left: 1px solid var(--border-strong); margin-left: 3px; }
  .timeline li:last-child { border-left-color: transparent; }
  .timeline b { color: var(--text); font-weight: 500; }
  .timeline .dot { position: absolute; left: -4px; top: 4px; width: 7px; height: 7px; border-radius: 50%; }
  .dot.intro { background: var(--sev-high); }
  .dot.seen { background: var(--text-3); }
  .dot.done { background: var(--good); }
  .dot.open { background: var(--sev-medium); }
  .tmsg { color: var(--text-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ref { font-size: 11px; margin-left: auto; }
</style>
