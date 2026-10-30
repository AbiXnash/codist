<script lang="ts">
  import { app, saveStr, selectCommit, showWorking } from '../lib/store.svelte';
  import { ago, SEVERITIES, SEVERITY_COLOR } from '../lib/theme';
  import Toggle from './Toggle.svelte';
  import type { CommitItem, Severity } from '../lib/types';

  let now = $state(Date.now());
  $effect(() => {
    const t = setInterval(() => (now = Date.now()), 60_000);
    return () => clearInterval(t);
  });

  function setHistory(v: 'mine' | 'everyone') {
    app.history = v;
    saveStr('history', v);
  }

  // What you have done, from the tracker: issues your commits introduced that are still open, and issues you fixed.
  const mine = $derived.by(() => {
    const all = app.issueList?.issues ?? [];
    const roots = (i: (typeof all)[number]) => !i.group || i.groupRoot || i.state !== 'open';
    return {
      commits: app.myCommits.length,
      open: all.filter((i) => i.state === 'open' && roots(i) && app.isMe(i.firstBy)).length,
      fixed: all.filter((i) => i.state === 'resolved' && app.isMe(i.fixedBy)).length,
    };
  });
  const initials = $derived((app.status?.user ?? '?').split(/\s+/).map((w) => w[0]).join('').slice(0, 2).toUpperCase());

  function summary(c: CommitItem): { total: number; worst: Severity | null } {
    let total = 0;
    let worst: Severity | null = null;
    for (const s of SEVERITIES) {
      const n = c.counts?.[s] ?? 0;
      total += n;
      if (n && !worst) worst = s;
    }
    return { total, worst };
  }

  function showMine(what: 'open' | 'resolved') {
    app.scope = 'mine';
    app.issueState = what;
    app.level = null;
    app.picked = null;
    app.tab = 'issues';
    app.showPanel = true;
  }
</script>

<aside class="rail">
  <div class="head">
    <h2>History</h2>
    <Toggle bind:checked={app.follow} label="Follow" title="Select each new commit as it arrives" />
  </div>

  {#if app.status?.user}
    <div class="me">
      <span class="avatar" aria-hidden="true">{initials}</span>
      <div class="who">
        <b>{app.status.user}</b>
        <span class="faint">you · from git config</span>
      </div>
    </div>
    <div class="stats">
      <button onclick={() => setHistory('mine')} title="List only your commits"><b>{mine.commits}</b><span>commits</span></button>
      <button onclick={() => showMine('open')} title="Open issues your commits introduced"><b class:warn={mine.open > 0}>{mine.open}</b><span>open by you</span></button>
      <button onclick={() => showMine('resolved')} title="Issues you fixed"><b class="good">{mine.fixed}</b><span>fixed by you</span></button>
    </div>
  {/if}

  <div class="seg" role="tablist" aria-label="Whose commits">
    <button role="tab" aria-selected={app.history === 'mine'} class:on={app.history === 'mine'} disabled={!app.me} onclick={() => setHistory('mine')}>Mine</button>
    <button role="tab" aria-selected={app.history === 'everyone'} class:on={app.history === 'everyone'} onclick={() => setHistory('everyone')}>Everyone</button>
  </div>

  <ol class="timeline">
    {#if app.working?.changes.length}
      {@const n = app.working.findings}
      <li class:selected={app.scope === 'working'}>
        <span class="node hollow" style:--c={n ? 'var(--warn)' : 'var(--good)'}></span>
        <button class="item working" onclick={showWorking}>
          <span class="msg">Uncommitted changes</span>
          <span class="meta"><span>{app.working.changes.length} file{app.working.changes.length === 1 ? '' : 's'} changed</span><span class="time">now</span></span>
          <span class="badges">
            {#if n}<span class="pill warn">{n} suggestion{n === 1 ? '' : 's'}</span>{:else}<span class="pill good">clean so far</span>{/if}
            {#if (app.issueList?.issues ?? []).some((i) => i.uncommitted)}<span class="pill good">✓ {(app.issueList?.issues ?? []).filter((i) => i.uncommitted).length} fixed locally</span>{/if}
          </span>
        </button>
      </li>
    {/if}
    {#each app.shownCommits as c, i (c.commit.sha)}
      {@const s = summary(c)}
      {@const selected = c.commit.sha === app.selectedSha && app.scope !== 'working'}
      <li class:selected class:last={i === app.shownCommits.length - 1}>
        <span class="node" style:--c={s.worst ? SEVERITY_COLOR[s.worst] : c.status === 'done' ? 'var(--good)' : 'var(--text-3)'}
          class:hollow={c.status !== 'done'} class:spin={c.status === 'running' || c.status === 'queued'}></span>
        <button class="item" onclick={() => selectCommit(c.commit.sha, true)}>
          <span class="msg">{c.commit.message}</span>
          <span class="meta">
            <span class="mono sha">{c.commit.shortSha}</span>
            {#if app.history === 'everyone'}<span class="author" class:me={app.isMe(c.commit.author)}>{app.isMe(c.commit.author) ? 'you' : c.commit.author}</span>{/if}
            <span class="time">{ago(c.commit.time, now)}</span>
          </span>
          <span class="badges">
            {#if c.status === 'running' || c.status === 'queued'}<span class="pill faint">{c.status === 'queued' ? 'queued' : 'reviewing…'}</span>
            {:else if c.status === 'failed'}<span class="pill bad">review failed</span>
            {:else if !c.status}<span class="pill faint">not reviewed</span>{/if}
            {#each SEVERITIES as sv}{#if c.counts?.[sv]}<span class="pill" style:--c={SEVERITY_COLOR[sv]} title="{c.counts[sv]} new {sv}">{c.counts[sv]} {sv}</span>{/if}{/each}
            {#if c.status === 'done' && !s.total}<span class="pill good">clean</span>{/if}
            {#if c.fixed}<span class="pill good" title="Earlier issues fixed by this commit">✓ {c.fixed} fixed</span>{/if}
            {#if c.messageProblem}<span class="pill warn" title="The commit message could be clearer">message</span>{/if}
          </span>
        </button>
      </li>
    {:else}
      <li class="empty">{app.history === 'mine' ? `No commits by ${app.status?.user ?? 'you'} yet.` : 'No commits yet.'}</li>
    {/each}
  </ol>
</aside>

<style>
  .rail { display: flex; flex-direction: column; min-height: 0; border-right: 1px solid var(--border); background: var(--surface); }
  .head { display: flex; justify-content: space-between; align-items: center; height: 44px; padding: 0 14px; flex-shrink: 0; }
  h2 { margin: 0; font-size: 13px; font-weight: 600; }

  .me { display: flex; align-items: center; gap: 10px; padding: 4px 14px 10px; }
  .avatar { width: 30px; height: 30px; border-radius: 50%; display: grid; place-items: center; font: 600 11.5px var(--font); color: var(--accent-text); background: var(--accent-soft); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--accent) 35%, transparent); }
  .who { display: grid; line-height: 1.3; min-width: 0; }
  .who b { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .who span { font-size: 11px; }
  .stats { display: grid; grid-template-columns: repeat(3, 1fr); gap: 6px; padding: 0 14px 12px; }
  .stats button { display: grid; gap: 1px; padding: 7px 8px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); text-align: left; }
  .stats button:hover { border-color: var(--border-strong); background: var(--surface-3); }
  .stats b { font-size: 16px; font-weight: 600; font-variant-numeric: tabular-nums; }
  .stats b.warn { color: var(--warn); }
  .stats b.good { color: var(--good); }
  .stats span { font-size: 10.5px; color: var(--text-3); white-space: nowrap; }

  .seg { display: flex; margin: 0 14px 6px; padding: 2px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .seg button { flex: 1; height: 24px; border: 0; border-radius: var(--r); background: none; color: var(--text-3); font-weight: 500; font-size: 12px; }
  .seg button.on { color: var(--text); background: var(--surface-3); box-shadow: 0 0 0 1px var(--border-strong); }
  .seg button:disabled { opacity: 0.4; cursor: default; }

  .timeline { list-style: none; margin: 0; padding: 6px 8px 16px; overflow: auto; flex: 1; }
  .timeline li { position: relative; padding-left: 20px; }
  /* The rail line between commit nodes. */
  .timeline li::before { content: ''; position: absolute; left: 9px; top: 20px; bottom: -12px; width: 1px; background: var(--border-strong); }
  .timeline li.last::before { display: none; }
  .node { position: absolute; left: 5px; top: 13px; width: 9px; height: 9px; border-radius: 50%; background: var(--c); box-shadow: 0 0 0 3px var(--surface); z-index: 1; }
  .node.hollow { background: var(--surface); border: 1.5px solid var(--c); }
  .node.spin { border-color: var(--accent); border-right-color: transparent; animation: spin 0.8s linear infinite; }
  .item { display: grid; gap: 3px; width: 100%; padding: 8px 10px; border: 1px solid transparent; border-radius: var(--r-lg); background: none; text-align: left; }
  .item:hover { background: var(--hover); }
  .selected .item { background: var(--accent-soft); border-color: color-mix(in srgb, var(--accent) 35%, transparent); }
  .item.working { border-style: dashed; border-color: var(--border-strong); }
  .msg { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
  .meta { display: flex; align-items: center; gap: 8px; min-width: 0; font-size: 11.5px; color: var(--text-3); }
  .sha { font-size: 11px; color: var(--text-2); }
  .author { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .author.me { color: var(--accent-text); }
  .time { margin-left: auto; white-space: nowrap; }
  .badges { display: flex; flex-wrap: wrap; gap: 4px; }
  .badges:empty { display: none; }
  .pill { --c: var(--text-3); height: 17px; padding: 0 6px; display: inline-flex; align-items: center; border-radius: 9px; font: 600 10px var(--font); text-transform: capitalize; color: var(--c); background: color-mix(in srgb, var(--c) 13%, transparent); white-space: nowrap; }
  .pill.good { --c: var(--good); }
  .pill.warn { --c: var(--warn); }
  .pill.bad { --c: var(--bad); }
  .pill.faint { text-transform: none; font-weight: 500; }
  .empty { padding: 32px 12px; color: var(--text-3); text-align: center; }
  .timeline li.empty::before { display: none; }
  @media (max-width: 1000px) { .rail { max-height: 420px; border-right: 0; border-bottom: 1px solid var(--border); } }
</style>
