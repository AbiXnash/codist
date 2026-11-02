<script lang="ts">
  import { app, pickIssue } from '../lib/store.svelte';
  import type { DevProfile, Issue } from '../lib/types';

  let { p }: { p: DevProfile } = $props();

  const num = (n: number) => n.toLocaleString();
  const max = (list: { lines: number }[]) => Math.max(1, ...list.map((s) => s.lines));
  const byId = $derived(new Map((app.issueList?.issues ?? []).map((i) => [i.id, i] as const)));
  const issue = (id: string): Issue | undefined => byId.get(id);
  function open(id: string) {
    app.mode = 'review';
    pickIssue(id);
  }
</script>

<div class="profile">
  <header>
    <h2>{p.name}</h2>
    <p class="muted">{p.reviewed} of {p.commits} commits reviewed.{#if !p.enoughEvidence}{' '}Not enough reviewed work to say much about quality yet, so this mostly describes what they did.{/if}</p>
  </header>

  <div class="cards">
    <div class="stat"><b>{num(p.added)}</b><span>lines written</span></div>
    <div class="stat"><b>{num(p.owned)}</b><span>lines in the code today</span></div>
    <div class="stat"><b>{p.testShare}%</b><span>of their lines are tests</span></div>
    <div class="stat"><b>{p.openNow}</b><span>open issues from their commits</span></div>
    <div class="stat"><b>{p.fixed}</b><span>issues fixed</span></div>
  </div>

  <div class="split">
    <section class="pcard">
      <h3>Does well</h3>
      {#each p.strengths as s}
        <div class="point"><b>{s.title}</b><p>{s.detail}</p></div>
      {:else}<p class="faint">Nothing stands out yet. This needs more reviewed work, or simply has not come up.</p>{/each}
    </section>
    <section class="pcard">
      <h3>To work on</h3>
      {#each p.improve as s}
        <div class="point">
          <b>{s.title}</b><p>{s.detail}</p>
          {#if s.advice}<p class="advice">{s.advice}</p>{/if}
          {#each s.issues ?? [] as id}
            {#if issue(id)}<button class="lnk ex" onclick={() => open(id)}>{issue(id)?.title} <span class="faint mono">{issue(id)?.path.split('/').pop()}</span></button>{/if}
          {/each}
        </div>
      {:else}<p class="faint">{p.reviewed ? 'Nothing to flag from what has been reviewed.' : 'Review their commits to see this.'}</p>{/each}
    </section>

    <section class="pcard">
      <h3>Skills</h3>
      <p class="faint hint">The kind of code they write, by lines added. This shows experience, not how well it is done.</p>
      {#each p.skills as s}
        <div class="bar-row" title="{s.commits} commit{s.commits === 1 ? '' : 's'}"><span>{s.name}</span><i style:width="{(s.lines / max(p.skills)) * 100}%"></i><b>{num(s.lines)}</b></div>
      {:else}<p class="faint">No code changes on this branch.</p>{/each}
      <h3 class="next">Where they work</h3>
      {#each p.areas.slice(0, 6) as a}
        <div class="bar-row"><span class="mono" title={a.name}>{a.name}</span><i style:width="{(a.lines / max(p.areas)) * 100}%"></i><b>{num(a.lines)}</b></div>
      {/each}
    </section>
    <section class="pcard">
      <h3>Bugs from their commits</h3>
      {#if p.introduced}
        <div class="row">{#each p.severity as s}<span class="sev {s.name}">{s.n} {s.name}</span>{/each}</div>
        <div class="row kinds">{#each p.kinds as k}<span class="tag">{k.name} {k.n}</span>{/each}</div>
      {:else}<p class="faint">{p.reviewed ? `None found in the ${p.reviewed} reviewed commits.` : 'None of their commits have been reviewed yet.'}</p>{/if}
      <h3 class="next">Fixes</h3>
      {#if p.fixed}
        <p class="line">{p.fixedOwn} from their own commits, {p.fixedOthers} from colleagues'.{#if p.avgFixDays} {p.avgFixDays < 1 ? 'Usually within a day.' : `About ${p.avgFixDays.toFixed(1)} days on average.`}{/if}</p>
      {:else}<p class="faint">No fixes yet.</p>{/if}
      <h3 class="next">Habits</h3>
      <p class="line">A typical commit changes {num(p.medianCommit)} lines; {p.bigCommits} {p.bigCommits === 1 ? 'was' : 'were'} over 400. {num(p.files)} files changed, {num(p.removed)} lines removed.</p>
    </section>
  </div>

  <p class="faint foot">Measured from git and Codist's reviews on this branch. Lines do not measure effort, and hard code produces more findings. Use this to start a conversation, not to rate someone.</p>
</div>

<style>
  .profile { display: grid; gap: 14px; min-width: 0; }
  header h2 { margin: 0 0 2px; font-size: 19px; font-weight: 700; }
  header p { margin: 0; }
  h3 { margin: 0 0 8px; font-size: 13px; font-weight: 600; }
  h3.next { margin-top: 14px; }
  .point { padding: 7px 0; border-top: 1px solid var(--border); }
  h3 + .point { border-top: 0; padding-top: 0; }
  .point b { font-weight: 500; }
  .point p { margin: 1px 0 0; font-size: 12.5px; color: var(--text-2); }
  .point .advice { color: var(--text); }
  .ex { display: block; margin-top: 3px; font-size: 12px; }
  .hint { margin: 0 0 8px; font-size: 12px; }
  .row { display: flex; flex-wrap: wrap; gap: 5px; }
  .kinds { margin-top: 7px; }
  .tag { padding: 0 7px; line-height: 19px; border-radius: 5px; font-size: 11.5px; background: var(--surface-3); }
  .line { margin: 0; font-size: 12.5px; color: var(--text-2); }
  .foot { margin: 0 4px; font-size: 12px; }
  .bar-row { grid-template-columns: 150px 1fr 44px; }
  .bar-row span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .bar-row b { text-align: right; font-weight: 500; font-variant-numeric: tabular-nums; }
</style>
