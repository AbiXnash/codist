<script lang="ts">
  import { app, loadCommits, openFile, requestReview, selectCommit, selectNode, toast } from '../lib/store.svelte';
  import { ago, SEVERITIES, shortName } from '../lib/theme';
  import FindingCard from './FindingCard.svelte';
  import CommitRef from './CommitRef.svelte';
  import CopyButton from './CopyButton.svelte';
  import ImpactCard from './ImpactCard.svelte';
  import { api } from '../lib/api';

  const d = $derived(app.detail);
  const r = $derived(d?.review ?? null);
  // One count per issue, at its worst severity, matching the issue list and the commit badges.
  const worstOf = (g: { severity: string }[]) => SEVERITIES.find((s) => g.some((f) => f.severity === s)) ?? 'info';
  const counts = $derived(
    SEVERITIES.map((s) => [s, app.issues.filter((g) => !g[0].ignored && worstOf(g) === s).length] as const).filter(([, n]) => n),
  );
  const ignoredCount = $derived(app.allFindings.length - app.findings.length);
  let showCarried = $state(false);

  let draft = $state('');
  let draftWhy = $state('');
  let pushed = $state(false);
  let msgBusy = $state(false);
  let draftFor = '';
  // The model's earlier suggestion, if the review made one; a new commit starts over.
  $effect(() => {
    const sha = app.detail?.analysis.commit.sha ?? '';
    if (sha !== draftFor) {
      draftFor = sha;
      draft = app.detail?.review?.suggestedMessage ?? '';
      draftWhy = '';
      pushed = false;
    }
  });
  async function suggest(sha: string) {
    msgBusy = true;
    try {
      const m = await api.betterMessage(sha);
      draft = m.message;
      draftWhy = m.why;
      pushed = m.pushed;
    } catch (e) {
      if ((e as Error).name !== 'AbortError') toast((e as Error).message, 'bad');
    } finally {
      msgBusy = false;
    }
  }
  async function reword(sha: string) {
    msgBusy = true;
    try {
      const res = await api.reword(sha, draft.trim());
      toast(res.rewritten > 1 ? `Reworded; ${res.rewritten} commits got new ids` : 'Reworded', 'good', 3000);
      draft = '';
      await loadCommits();
      await selectCommit(res.commit);
    } catch (e) {
      toast((e as Error).message, 'bad');
    } finally {
      msgBusy = false;
    }
  }

  function focus(id: string) {
    selectNode(id);
    window.dispatchEvent(new CustomEvent('codist:center', { detail: id }));
  }
</script>

{#if !d}
  <div class="empty">Select a commit.</div>
{:else}
  {@const c = d.analysis.commit}
  <div class="review">
    <!-- On Review the centre of the screen already shows the commit's title and its issues. -->
    {#if app.mode !== 'review'}
    <header>
      <h1>{c.message}</h1>
      <div class="meta">
        <span class="mono">{c.shortSha}</span><CopyButton text={c.sha} title="Copy the full commit id" />
        <span>{c.author}</span>
        <span title={new Date(c.time).toLocaleString()}>{ago(c.time)}</span>
        <span>{d.analysis.files.length} file{d.analysis.files.length === 1 ? '' : 's'}</span>
      </div>
    </header>
    {/if}

    <section class="status">
      {#if !r}
        <span class="muted">Not reviewed.</span>
        <button class="btn sm" onclick={() => requestReview(c.sha)}>Review</button>
      {:else if r.status === 'queued' || r.status === 'running'}
        <span class="spinner"></span>
        <span class="muted">
          {#if r.status === 'queued'}Queued{:else}
            {@const pending = r.files.filter((f) => f.pending).length}
            Rules done for {r.files.length} file{r.files.length === 1 ? '' : 's'}{pending ? ' · AI reviewing ' + r.files.find((f) => f.pending)?.path.split('/').pop() : ''}
          {/if}
        </span>
      {:else if r.status === 'failed'}
        <span class="bad">Review failed</span>
        <button class="btn secondary sm" onclick={() => requestReview(c.sha)}>Retry</button>
      {:else}
        <span class="risk {r.risk}">{r.risk} risk</span>
        {#each counts as [s, n]}<span class="sev {s}">{n} {s}</span>{:else}<span class="clean">No issues</span>{/each}
        <span class="spacer"></span>
        {#if ignoredCount}
          <button class="link" onclick={() => (app.showIgnored = !app.showIgnored)}>{app.showIgnored ? 'Hide' : 'Show'} {ignoredCount} ignored</button>
        {/if}
        <button class="btn secondary sm" title="Run the review again" onclick={() => requestReview(c.sha)}>Re-run</button>
      {/if}
    </section>

    {#if r?.status === 'failed' && r.error}<p class="error mono">{r.error}</p>{/if}
    {#if r?.status === 'done' && r.summary}<p class="summary">{r.summary}</p>{/if}
    {#if r?.analysers?.length || r?.analysisProblems?.length}
      <p class="checked faint" title={(r.analysisProblems ?? []).join('\n')}>
        Checked by Codist rules, the knowledge base{#each r.analysers ?? [] as t}, {t === 'pmd' ? 'PMD' : 'SpotBugs'}{/each} and the model
        {#if r.analysisProblems?.length}<span class="warnc"> · {r.analysisProblems.length} analyser problem{r.analysisProblems.length > 1 ? 's' : ''}</span>{/if}
      </p>
    {/if}

    {#if d.analysis.changed.length}
      <section class="types">
        {#if d.analysis.changed.length}
          <div class="line">
            <span class="label">Changed</span>
            <div class="chips">
              {#each d.analysis.changed as t (t.id)}
                <button class="chip" disabled={t.status === 'deleted'} class:deleted={t.status === 'deleted'} title={t.id} onclick={() => focus(t.id)}>
                  <span class="dot" style:background={t.status === 'added' ? 'var(--added)' : t.status === 'deleted' ? 'var(--bad)' : 'var(--changed)'}></span>{shortName(t.id)}
                </button>
              {/each}
            </div>
          </div>
        {/if}
      </section>
    {/if}

    <ImpactCard sha={c.sha} />

    <section class="card msg">
      <div class="card-head"><span class="label">Commit message</span><span class="spacer"></span>
        <CopyButton text={c.message} title="Copy this commit's message" />
        {#if app.featureOn('reword')}<button class="btn secondary sm" disabled={msgBusy} onclick={() => suggest(c.sha)}>{#if msgBusy}<span class="spinner"></span>{/if}{draft ? 'Suggest again' : 'Suggest a better one'}</button>{/if}
      </div>
      {#if r?.messageProblem}<p class="problem">{r.messageProblem}</p>{/if}
      {#if draft}
        <textarea class="mono" rows="3" bind:value={draft} spellcheck="false"></textarea>
        {#if draftWhy}<p class="faint">{draftWhy}</p>{/if}
        <div class="row-actions">
          <button class="btn sm" style:display={app.featureOn('reword') ? undefined : 'none'} disabled={pushed || msgBusy || !draft.trim() || draft.trim() === c.message.trim()} title={pushed ? 'This commit is already pushed; rewriting it would change history for everyone' : 'Rewrites this commit and the ones after it, keeping their code and authors'} onclick={() => reword(c.sha)}>Reword commit</button>
          <CopyButton text={draft} label="Copy" />
          {#if pushed}<span class="faint">Already pushed, so it cannot be reworded here.</span>{/if}
        </div>
      {/if}
    </section>

    {#if r}
      {@const pending = r.files.filter((f) => f.pending)}
      {@const errors = r.files.filter((f) => f.error)}
      <section class="issues">
        <div class="issues-head" class:hidden={app.mode === 'review'}>
          <span class="label">New in this commit · {app.issues.length}</span>
          {#if app.findings.length > app.issues.length + app.carried.length}<span class="faint" title="Findings with the same root cause are shown once">grouped from {app.findings.length} findings</span>{/if}
        </div>
        {#if app.mode !== 'review'}
        {#each app.issues as group (group[0].path + group[0].line + group[0].title)}
          <FindingCard f={group[0]} members={group} />
        {:else}
          {#if r.status === 'done' && !pending.length}<p class="faint none">No new issues.</p>{/if}
        {/each}
        {/if}
        {#each pending as f (f.path)}<p class="faint pending"><span class="spinner inline"></span>AI reviewing {f.path.split('/').pop()}…</p>{/each}
        {#each errors as f (f.path)}<p class="error mono">{f.path.split('/').pop()}: {f.error}</p>{/each}
      </section>

      {#if d.resolved?.length}
        <section class="issues fixed">
          <div class="issues-head"><span class="label">✓ Fixed in this commit · {d.resolved.length}</span><span class="faint">by {c.author}</span></div>
          {#each d.resolved as is (is.id)}
            <div class="fixed-row">
              <span class="sev {is.severity}">{is.severity}</span>
              <span class="ftitle">{is.title}</span>
              <span class="mono floc">{is.path.split('/').pop()}</span>
              <div class="fsub">introduced in <CommitRef sha={is.firstSeen} /> by {is.firstBy} · {ago(is.firstTime)}{is.commits.length > 1 ? ` · open across ${is.commits.length} reviews` : ''}</div>
            </div>
          {/each}
        </section>
      {/if}

      {#if app.carried.length}
        <section class="issues carried">
          <button class="issues-head toggle" onclick={() => (showCarried = !showCarried)} aria-expanded={showCarried}>
            <span class="label">{showCarried ? '▾' : '▸'} From earlier commits · {app.carried.length}</span>
            <span class="faint">still in the changed files</span>
          </button>
          {#if showCarried}
            {#each app.carried as group (group[0].path + group[0].line + group[0].title)}
              <FindingCard f={group[0]} members={group} />
            {/each}
          {/if}
        </section>
      {/if}
    {/if}
  </div>
{/if}

<style>
  .checked { margin: 0 0 8px; font-size: 11.5px; }
  .checked .warnc { color: var(--warn); }
  .review { padding: 16px; }
  h1 { margin: 0 0 4px; font-size: 15px; font-weight: 600; line-height: 1.35; }
  .meta { display: flex; flex-wrap: wrap; gap: 4px 12px; font-size: 12px; color: var(--text-3); }
  .meta .mono { font-size: 11.5px; color: var(--text-2); }

  .status { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin: 14px 0 0; padding: 10px 0; border-top: 1px solid var(--border); border-bottom: 1px solid var(--border); }
  .spacer { flex: 1; }
  .risk { font-size: 12px; font-weight: 600; text-transform: capitalize; margin-right: 4px; }
  .risk.low { color: var(--good); }
  .risk.medium { color: var(--warn); }
  .risk.high { color: var(--bad); }
  .clean { color: var(--good); font-size: 12px; }
  .bad { color: var(--bad); }
  .summary { margin: 12px 0 0; color: var(--text-2); line-height: 1.6; }
  .error { margin: 10px 0 0; color: var(--bad); white-space: pre-wrap; font-size: 11.5px; }

  .types { display: grid; gap: 8px; margin-top: 14px; }
  .line { display: grid; grid-template-columns: 72px 1fr; align-items: start; gap: 8px; }
  .line .label { line-height: 22px; }
  .chips { display: flex; flex-wrap: wrap; gap: 4px; }
  .chip:disabled { cursor: default; }
  .chip.deleted { text-decoration: line-through; color: var(--text-3); }

  .issues { margin-top: 18px; }
  .carried { margin-top: 14px; }
  .fixed .issues-head .label { color: var(--good); }
  .fixed-row { display: grid; grid-template-columns: auto 1fr auto; align-items: center; gap: 4px 8px; padding: 8px 4px; border-bottom: 1px solid var(--border); }
  .fixed-row .ftitle { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-decoration: line-through; text-decoration-color: color-mix(in srgb, var(--text-3) 60%, transparent); }
  .floc { font-size: 11px; color: var(--text-3); }
  .fsub { grid-column: 2 / 4; display: flex; align-items: center; gap: 4px; flex-wrap: wrap; font-size: 11.5px; color: var(--text-3); }
  .toggle { width: 100%; border: 0; border-left: 0; border-right: 0; border-top: 0; background: none; text-align: left; cursor: pointer; }
  .none { margin: 10px 4px; font-size: 12px; }
  .card { margin-top: 14px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .card.msg { border-left: 2px solid var(--warn); }
  .card-head { margin-bottom: 4px; }
  .problem { margin: 0 0 8px; color: var(--text-2); font-size: 12.5px; }
  .suggested { margin: 0 0 8px; padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--r); background: var(--code-bg); white-space: pre-wrap; font-size: 12px; color: var(--text); }
  .row-actions { display: flex; gap: 6px; }
  .issues-head { display: flex; justify-content: space-between; align-items: baseline; padding: 0 4px 6px; border-bottom: 1px solid var(--border); }
  .pending { margin: 8px 4px 0; font-size: 12px; }
  .file { margin-top: 18px; }
  .file-head { display: flex; justify-content: space-between; align-items: center; padding: 0 4px 6px; border-bottom: 1px solid var(--border); }
  .path { border: 0; padding: 0; background: none; color: var(--accent-text); font-size: 12px; }
  .path:hover { text-decoration: underline; }
  .link { padding: 0; border: 0; background: none; font-size: 12px; color: var(--accent-text); }
  .spinner.inline { display: inline-block; width: 10px; height: 10px; vertical-align: -1px; margin-right: 4px; }
  .msg textarea { width: 100%; margin-top: 8px; padding: 7px 9px; border: 1px solid var(--border-strong); border-radius: var(--r); background: var(--surface-2); color: var(--text); font-size: 12px; resize: vertical; }
  .msg .row-actions { align-items: center; }
  .card-head .spacer { flex: 1; }
  .issues-head.hidden { display: none; }
  .status:first-child { margin-top: 0; padding-top: 0; border-top: 0; }
</style>
