<script lang="ts">
  import WorkingPlan from './WorkingPlan.svelte';
  import { app, pickIssue, saveStr, selectCommit, showWorking } from '../lib/store.svelte';
  import { ago, SEVERITIES, SEVERITY_COLOR } from '../lib/theme';
  import Toggle from './Toggle.svelte';
  import type { CommitItem, Issue, Severity } from '../lib/types';

  let query = $state('');
  let filter = $state<'all' | 'open' | 'clean' | 'fixed' | 'unreviewed'>('all');
  let author = $state('');
  let open = $state<Record<string, boolean>>({});

  const issues = $derived(app.issueList?.issues ?? []);
  // One entry per commit: what it introduced (and what became of it) and what it fixed.
  const related = $derived.by(() => {
    const m = new Map<string, { introduced: Issue[]; fixed: Issue[] }>();
    const at = (sha: string) => {
      if (!m.has(sha)) m.set(sha, { introduced: [], fixed: [] });
      return m.get(sha)!;
    };
    for (const i of issues) {
      at(i.firstSeen).introduced.push(i);
      if (i.fixedIn) at(i.fixedIn).fixed.push(i);
    }
    return m;
  });
  const none = { introduced: [] as Issue[], fixed: [] as Issue[] };
  const rel = (c: CommitItem) => related.get(c.commit.sha) ?? none;
  const openOf = (c: CommitItem) => rel(c).introduced.filter((i) => i.state === 'open').length;

  const authors = $derived([...new Set(app.commits.map((c) => c.commit.author))].sort());
  const shown = $derived.by(() => {
    const q = query.trim().toLowerCase();
    return app.shownCommits.filter((c) => {
      if (author && c.commit.author !== author) return false;
      const r = rel(c);
      if (filter === 'open' && !openOf(c)) return false;
      if (filter === 'clean' && (c.status !== 'done' || r.introduced.length)) return false;
      if (filter === 'fixed' && !r.fixed.length) return false;
      if (filter === 'unreviewed' && c.status === 'done') return false;
      if (!q) return true;
      return `${c.commit.message} ${c.commit.shortSha} ${c.commit.author}`.toLowerCase().includes(q) ||
        r.introduced.some((i) => `${i.title} ${i.path} ${i.kind ?? ''} ${i.ref ?? ''}`.toLowerCase().includes(q));
    });
  });

  function setHistory(v: 'mine' | 'everyone') {
    app.history = v;
    saveStr('history', v);
    author = '';
  }
  function worst(c: CommitItem): Severity | null {
    return SEVERITIES.find((s) => c.counts?.[s]) ?? null;
  }
  function pick(c: CommitItem) {
    void selectCommit(c.commit.sha, true);
    open[c.commit.sha] = !open[c.commit.sha];
  }
  const name = (p: string) => p.slice(p.lastIndexOf('/') + 1);
  const stateText = (i: Issue) => (i.state === 'resolved' ? (i.fixedIn ? `fixed in ${i.fixedIn.slice(0, 7)}` : 'fixed') : i.state === 'ignored' ? 'ignored' : i.reopenedIn ? 'reopened' : 'open');
</script>

<div class="commits">
  <div class="top">
    <div class="row">
      <div class="seg" role="tablist" aria-label="Whose commits">
        <button role="tab" aria-selected={app.history === 'mine'} class:on={app.history === 'mine'} disabled={!app.me} onclick={() => setHistory('mine')}>Mine</button>
        <button role="tab" aria-selected={app.history === 'everyone'} class:on={app.history === 'everyone'} onclick={() => setHistory('everyone')}>Everyone</button>
      </div>
      <Toggle bind:checked={app.follow} label="Follow" title="Select each new commit as it arrives" />
    </div>
    <input class="q" bind:value={query} placeholder="Search commits, authors, shas, issues, files…" spellcheck="false" />
    <div class="row">
      <select bind:value={filter} aria-label="Filter commits">
        <option value="all">All commits</option>
        <option value="open">With open issues</option>
        <option value="clean">Clean</option>
        <option value="fixed">That fixed something</option>
        <option value="unreviewed">Not reviewed</option>
      </select>
      {#if app.history === 'everyone'}
        <select bind:value={author} aria-label="Filter by author">
          <option value="">Any author</option>
          {#each authors as a}<option value={a}>{a}</option>{/each}
        </select>
      {/if}
      <span class="faint n">{shown.length} of {app.shownCommits.length}</span>
    </div>
  </div>

  <ol class="timeline">
    {#if app.working?.changes.length}
      {@const n = app.working.findings}
      <li class:selected={app.scope === 'working'}>
        <span class="node hollow" style:--c={n ? 'var(--warn)' : 'var(--good)'}></span>
        <button class="item working" onclick={showWorking}>
          <span class="msg">Uncommitted changes</span>
          <span class="meta"><span>{app.working.changes.length} file{app.working.changes.length === 1 ? '' : 's'} changed</span><span class="time">now</span></span>
          <span class="badges">{#if n}<span class="pill warn">{n} suggestion{n === 1 ? '' : 's'}</span>{:else}<span class="pill good">clean so far</span>{/if}</span>
        </button>
        {#if app.scope === 'working' && app.featureOn('localPlan')}<WorkingPlan />{/if}
      </li>
    {/if}
    {#each shown as c, idx (c.commit.sha)}
      {@const r = rel(c)}
      {@const w = worst(c)}
      {@const selected = c.commit.sha === app.selectedSha && app.scope !== 'working'}
      {@const openCount = openOf(c)}
      <li class:selected class:last={idx === shown.length - 1}>
        <span class="node" style:--c={w ? SEVERITY_COLOR[w] : c.status === 'done' ? 'var(--good)' : 'var(--text-3)'} class:hollow={c.status !== 'done'} class:spin={c.status === 'running' || c.status === 'queued'}></span>
        <div class="card">
          <button class="item" aria-expanded={!!open[c.commit.sha]} onclick={() => pick(c)}>
            <span class="msg">{c.commit.message}</span>
            <span class="meta">
              <span class="mono sha">{c.commit.shortSha}</span>
              <span class="author" class:me={app.isMe(c.commit.author)}>{app.isMe(c.commit.author) ? 'you' : c.commit.author}</span>
              <span class="time">{ago(c.commit.time)}</span>
            </span>
            <span class="badges">
              {#if c.status === 'running' || c.status === 'queued'}<span class="pill faint">{c.status === 'queued' ? 'queued' : 'reviewing…'}</span>
              {:else if c.status === 'failed'}<span class="pill bad">review failed</span>
              {:else if !c.status}<span class="pill faint">not reviewed</span>
              {:else if !r.introduced.length && !c.counts}<span class="pill good">clean</span>{/if}
              {#if openCount}<span class="pill bad">{openCount} open</span>{/if}
              {#if r.introduced.length - openCount > 0}<span class="pill good">{r.introduced.length - openCount} closed</span>{/if}
              {#if r.fixed.length}<span class="pill good">✓ fixed {r.fixed.length}</span>{/if}
              {#if c.messageProblem}<span class="pill warn" title="The commit message could be clearer">message</span>{/if}
            </span>
          </button>
          {#if open[c.commit.sha]}
            <div class="related">
              {#if r.introduced.length}
                <div class="h">Introduced here</div>
                {#each r.introduced as i (i.id)}
                  <button class="iss" onclick={() => pickIssue(i.id)} title={i.detail}>
                    <span class="sev {i.severity}">{i.severity}</span>
                    <span class="t">{i.title}</span>
                    <span class="st {i.state}">{stateText(i)}</span>
                    <span class="sub mono">{name(i.path)}{i.line ? ':' + i.line : ''}{i.kind ? ` · ${i.kind}` : ''}{i.urgency ? ` · ${i.urgency}` : ''}</span>
                  </button>
                {/each}
              {/if}
              {#if r.fixed.length}
                <div class="h">Fixed here</div>
                {#each r.fixed as i (i.id)}
                  <button class="iss" onclick={() => pickIssue(i.id)}>
                    <span class="sev {i.severity}">{i.severity}</span>
                    <span class="t">{i.title}</span>
                    <span class="st resolved">✓ from {i.firstSeen.slice(0, 7)}</span>
                    <span class="sub mono">{name(i.path)}{i.kind ? ` · ${i.kind}` : ''}</span>
                  </button>
                {/each}
              {/if}
              {#if !r.introduced.length && !r.fixed.length}<div class="faint none">{c.status === 'done' ? 'No issues tied to this commit.' : 'This commit has not been reviewed yet.'}</div>{/if}
              <div class="acts">
                <button class="lnk" onclick={() => (app.tab = 'review')}>Review</button>
                <button class="lnk" onclick={() => (app.tab = 'diff')}>Changes</button>
              </div>
            </div>
          {/if}
        </div>
      </li>
    {:else}
      <li class="empty">{query || filter !== 'all' ? 'No commits match.' : app.history === 'mine' ? `No commits by ${app.status?.user ?? 'you'} yet.` : 'No commits yet.'}</li>
    {/each}
  </ol>
</div>

<style>
  .commits { display: flex; flex-direction: column; min-height: 100%; }
  .top { position: sticky; top: 0; z-index: 2; display: grid; gap: 8px; padding: 10px 12px; border-bottom: 1px solid var(--border); background: var(--surface); }
  .row { display: flex; align-items: center; gap: 8px; justify-content: space-between; }
  .seg { display: flex; flex: 1; padding: 2px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .seg button { flex: 1; height: 24px; border: 0; border-radius: var(--r); background: none; color: var(--text-3); font-weight: 500; font-size: 12px; }
  .seg button.on { color: var(--text); background: var(--surface-3); box-shadow: 0 0 0 1px var(--border-strong); }
  .seg button:disabled { opacity: 0.4; cursor: default; }
  .q { height: 30px; padding: 0 10px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12.5px; }
  .q:focus { outline: none; border-color: var(--accent); }
  select { height: 26px; max-width: 170px; padding: 0 6px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12px; }
  .n { margin-left: auto; font-size: 11px; white-space: nowrap; }
  .timeline { list-style: none; margin: 0; padding: 8px 10px 20px 8px; }
  .timeline li { position: relative; padding-left: 20px; }
  .timeline li::before { content: ''; position: absolute; left: 9px; top: 20px; bottom: -12px; width: 1px; background: var(--border-strong); }
  .timeline li.last::before, .timeline li.empty::before { display: none; }
  .node { position: absolute; left: 5px; top: 13px; width: 9px; height: 9px; border-radius: 50%; background: var(--c); box-shadow: 0 0 0 3px var(--surface); z-index: 1; }
  .node.hollow { background: var(--surface); border: 1.5px solid var(--c); }
  .node.spin { border-color: var(--accent); border-right-color: transparent; animation: spin 0.8s linear infinite; }
  .card { border: 1px solid transparent; border-radius: var(--r-lg); margin-bottom: 2px; }
  .selected .card { background: var(--accent-soft); border-color: color-mix(in srgb, var(--accent) 35%, transparent); }
  .item { display: grid; gap: 3px; width: 100%; padding: 8px 10px; border: 0; border-radius: var(--r-lg); background: none; text-align: left; }
  .item:hover { background: var(--hover); }
  .item.working { border: 1px dashed var(--border-strong); }
  .msg { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
  .meta { display: flex; align-items: center; gap: 8px; min-width: 0; font-size: 11.5px; color: var(--text-3); }
  .sha { font-size: 11px; color: var(--text-2); }
  .author { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .author.me { color: var(--accent-text); }
  .time { margin-left: auto; white-space: nowrap; }
  .badges { display: flex; flex-wrap: wrap; gap: 4px; }
  .badges:empty { display: none; }
  .pill { --c: var(--text-3); height: 17px; padding: 0 6px; display: inline-flex; align-items: center; border-radius: 9px; font: 600 10px var(--font); color: var(--c); background: color-mix(in srgb, var(--c) 13%, transparent); white-space: nowrap; }
  .pill.good { --c: var(--good); } .pill.warn { --c: var(--warn); } .pill.bad { --c: var(--bad); }
  .pill.faint { font-weight: 500; }
  .related { padding: 0 10px 8px; display: grid; gap: 3px; }
  .h { margin-top: 4px; font-size: 10.5px; font-weight: 600; color: var(--text-3); text-transform: uppercase; letter-spacing: 0.04em; }
  .iss { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 1px 6px; align-items: center; padding: 5px 6px; border: 0; border-radius: var(--r); background: var(--surface-2); text-align: left; font-size: 12px; }
  .iss:hover { background: var(--surface-3); }
  .iss .t { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .iss .sub { grid-column: 1 / -1; font-size: 10.5px; color: var(--text-3); }
  .st { font: 600 10px var(--font); color: var(--bad); white-space: nowrap; }
  .st.resolved { color: var(--good); } .st.ignored { color: var(--text-3); }
  .none { font-size: 12px; }
  .acts { display: flex; gap: 10px; margin-top: 3px; }
  .lnk { border: 0; background: none; padding: 0; color: var(--accent-text); font-size: 11.5px; }
  .lnk:hover { text-decoration: underline; }
  .empty { padding: 32px 12px; color: var(--text-3); text-align: center; }
</style>
