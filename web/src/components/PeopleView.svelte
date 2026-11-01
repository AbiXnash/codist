<script lang="ts">
  import { app, pickIssue, selectCommit } from '../lib/store.svelte';
  import { ago, SEVERITIES } from '../lib/theme';
  import { api } from '../lib/api';
  import type { Issue, PeopleReport } from '../lib/types';

  let { mode }: { mode: 'mine' | 'team' } = $props();

  // Measured from git on the followed branch: reloaded when its head moves.
  let lines = $state.raw<PeopleReport | null>(null);
  $effect(() => {
    void app.status?.head;
    api.people().then((r) => (lines = r), () => {});
  });
  const linesOf = (name: string) => lines?.people.find((p) => p.name.trim().toLowerCase() === name.trim().toLowerCase());
  const num = (n?: number) => (n === undefined ? '…' : n.toLocaleString());
  const share = (n?: number) => (n && lines?.totalLines ? ` · ${Math.round((n / lines.totalLines) * 100)}%` : '');
  const mineLines = $derived(linesOf(app.status?.user ?? ''));

  const issues = $derived(app.issueList?.issues ?? []);
  const roots = $derived(issues.filter((i) => i.state !== 'open' || !i.group || i.groupRoot));
  const service = (p: string) => (p.includes('/src/') ? p.slice(0, p.indexOf('/src/')) : '(root)');

  function stats(name: string) {
    const eq = (n?: string) => (n ?? '').trim().toLowerCase() === name.trim().toLowerCase();
    const commits = app.commits.filter((c) => eq(c.commit.author));
    const introduced = roots.filter((i) => eq(i.firstBy));
    const open = introduced.filter((i) => i.state === 'open');
    const fixed = roots.filter((i) => i.state === 'resolved' && eq(i.fixedBy));
    const assigned = roots.filter((i) => i.state === 'open' && eq(i.assignee));
    const areas = new Map<string, number>();
    for (const i of introduced) areas.set(service(i.path), (areas.get(service(i.path)) ?? 0) + 1);
    const kinds = new Map<string, number>();
    for (const i of introduced) if (i.kind) kinds.set(i.kind, (kinds.get(i.kind) ?? 0) + 1);
    return { name, commits, introduced, open, fixed, assigned, last: commits[0]?.commit.time ?? 0,
      areas: [...areas].sort((a, b) => b[1] - a[1]), kinds: [...kinds].sort((a, b) => b[1] - a[1]),
      badMessages: commits.filter((c) => c.messageProblem).length };
  }
  const people = $derived([...new Set(app.commits.map((c) => c.commit.author))].map(stats).sort((a, b) => b.commits.length - a.commits.length));
  const me = $derived(stats(app.status?.user ?? ''));

  const urgencyOrder = ['Hotfix', 'Fix soon', 'Quick fix', 'Cleanup'];
  const todo = $derived(urgencyOrder.map((u) => ({ urgency: u, items: [...me.open, ...me.assigned.filter((a) => !me.open.includes(a))].filter((i) => i.urgency === u) })).filter((g) => g.items.length));
  const name = (p: string) => p.slice(p.lastIndexOf('/') + 1);
  function openIssue(i: Issue) { app.mode = 'review'; pickIssue(i.id); }
  function openCommit(sha: string) { app.mode = 'review'; app.left = 'commits'; app.showPanel = true; void selectCommit(sha, true); app.tab = 'review'; }
  const maxCommits = $derived(Math.max(1, ...people.map((p) => p.commits.length)));
  const unassigned = $derived(roots.filter((i) => i.state === 'open' && !i.assignee && (i.urgency === 'Hotfix' || i.urgency === 'Fix soon')));
</script>

<div class="page"><div class="page-in">
  {#if mode === 'mine'}
    <header><h1>Mine</h1><p>{app.status?.user || 'You'}: what you should do next, and what your commits did.</p></header>
    <div class="cards">
      <div class="stat"><b>{me.open.length}</b><span>open from your commits</span></div>
      <div class="stat"><b>{me.assigned.length}</b><span>assigned to you</span></div>
      <div class="stat"><b>{me.fixed.length}</b><span>fixed by you</span></div>
      <div class="stat"><b>{me.commits.length}</b><span>commits</span></div>
      <div class="stat"><b>{num(mineLines?.added)}</b><span>lines written</span></div>
      <div class="stat"><b>{num(mineLines?.removed)}</b><span>lines removed</span></div>
      <div class="stat" title="Lines in the code today that git blame gives to you"><b>{num(mineLines?.owned)}</b><span>lines of code today{share(mineLines?.owned)}</span></div>
      <div class="stat"><b>{num(mineLines?.files)}</b><span>files changed</span></div>
      <div class="stat"><b>{me.badMessages}</b><span>commit messages to improve</span></div>
    </div>
    <div class="split">
      <section class="pcard">
        <h2>To do, most urgent first</h2>
        {#each todo as g}
          <div class="u">{g.urgency} · {g.items.length}</div>
          {#each g.items as i (i.id)}
            <button class="row" onclick={() => openIssue(i)}><span class="sev {i.severity}">{i.severity}</span><span class="t">{i.title}</span><span class="faint mono s">{name(i.path)}</span></button>
          {/each}
        {:else}<p class="good">Nothing open from your commits.</p>{/each}
      </section>
      <section class="pcard">
        <h2>Your recent commits</h2>
        {#each me.commits.slice(0, 15) as c (c.commit.sha)}
          <button class="row" onclick={() => openCommit(c.commit.sha)}>
            <span class="mono s sha">{c.commit.shortSha}</span><span class="t">{c.commit.message}</span>
            {#if c.messageProblem}<span class="warn s">message</span>{/if}
            {#each SEVERITIES as sv}{#if c.counts?.[sv]}<span class="sev {sv}">{c.counts[sv]}</span>{/if}{/each}
            <span class="faint s">{ago(c.commit.time)}</span>
          </button>
        {:else}<p class="faint">No commits by you here.</p>{/each}
      </section>
    </div>
  {:else}
    <header><h1>Team</h1><p>Who works where, what is open and who has it.</p></header>
    <div class="cards">
      <div class="stat"><b>{people.length}</b><span>people</span></div>
      <div class="stat"><b>{roots.filter((i) => i.state === 'open').length}</b><span>open issues</span></div>
      <div class="stat"><b>{roots.filter((i) => i.state === 'open' && i.assignee).length}</b><span>being worked on</span></div>
      <div class="stat"><b class:warn={unassigned.length > 0}>{unassigned.length}</b><span>urgent, nobody on it</span></div>
      <div class="stat"><b>{roots.filter((i) => i.state === 'resolved').length}</b><span>fixed</span></div>
      <div class="stat"><b>{num(lines?.totalLines)}</b><span>lines of code</span></div>
    </div>
    <section class="pcard">
      <div style="overflow-x:auto"><table class="tbl">
        <thead><tr><th>Person</th><th>Commits</th><th class="num">Lines written</th><th class="num">Removed</th><th class="num" title="Lines in the code today that git blame gives to them">Lines today</th><th class="num">Files</th><th class="num">Open from their code</th><th class="num">Assigned</th><th class="num">Fixed</th><th>Works mostly in</th><th>Most common issues</th><th>Last commit</th></tr></thead>
        <tbody>
          {#each people as p}
            <tr>
              <td>{p.name}{#if app.isMe(p.name)} <span class="faint">you</span>{/if}</td>
              <td><div class="b"><i style:width="{(p.commits.length / maxCommits) * 100}%"></i><span>{p.commits.length}</span></div></td>
              <td class="num add">+{num(linesOf(p.name)?.added)}</td><td class="num del">−{num(linesOf(p.name)?.removed)}</td>
              <td class="num">{num(linesOf(p.name)?.owned)}<span class="faint">{share(linesOf(p.name)?.owned)}</span></td><td class="num">{num(linesOf(p.name)?.files)}</td>
              <td class="num">{p.open.length}</td><td class="num">{p.assigned.length}</td><td class="num">{p.fixed.length}</td>
              <td class="mono s">{p.areas.slice(0, 2).map(([a]) => a).join(', ') || '—'}</td>
              <td class="s">{p.kinds.slice(0, 2).map(([k, n]) => `${k} ${n}`).join(', ') || '—'}</td>
              <td class="faint s">{p.last ? ago(p.last) : '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table></div>
    </section>
    <section class="pcard">
      <h2>Urgent with nobody on it · {unassigned.length}</h2>
      {#each unassigned as i (i.id)}
        <button class="row" onclick={() => openIssue(i)}><span class="sev {i.severity}">{i.severity}</span><span class="t">{i.title}</span><span class="faint mono s">{name(i.path)}</span><span class="faint s">from {i.firstBy}</span></button>
      {:else}<p class="good">Every urgent issue has someone on it.</p>{/each}
    </section>
  {/if}
</div></div>

<style>
  .row { display: flex; align-items: center; gap: 8px; width: 100%; padding: 5px 4px; border: 0; border-radius: var(--r); background: none; text-align: left; font-size: 12.5px; }
  .row:hover { background: var(--hover); }
  .t { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .s { font-size: 11px; white-space: nowrap; }
  .sha { color: var(--accent-text); }
  .u { margin: 8px 0 2px; font-size: 11px; font-weight: 600; color: var(--text-2); }
  .good { margin: 0; color: var(--good); }
  .warn { color: var(--warn); }
  .b { display: flex; align-items: center; gap: 6px; min-width: 90px; }
  .b i { height: 7px; border-radius: 4px; background: var(--accent); }
  .add { color: var(--added); } .del { color: var(--bad); }
</style>
