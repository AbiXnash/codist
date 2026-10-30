<script lang="ts">
  import { app, ignoreFinding, openFile, openProposal, proposeFix, unignore } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import type { FindingWithPath, IgnoreScope } from '../lib/types';
  import CodeBlock from './CodeBlock.svelte';
  import CommitRef from './CommitRef.svelte';
  import { ago, NOT_FIXABLE } from '../lib/theme';

  // `members` are every place of the same issue (root first); a single finding is an issue of one.
  let { f, members = [f], showPath = true }: { f: FindingWithPath; members?: FindingWithPath[]; showPath?: boolean } = $props();
  let expanded = $state(false);
  let menu = $state(false);
  let team = $state(false);

  const files = $derived([...new Set(members.map((m) => m.path))]);
  // Prefer a rule's explanation and fix over the model's: they are precise and what Fix applies.
  const lead = $derived(members.find((m) => m.source === 'rule' && m.suggestion) ?? f);
  // "Root cause" only means something when the places are different findings, not repeats of one rule.
  const mixed = $derived(new Set(members.map((m) => m.title)).size > 1);
  // "instant" = mechanical fix, "ready" = prepared by the model in the background.
  const readiness = $derived(app.detail?.fixes?.[`${f.path}|${f.line}|${f.title}`]);
  const carried = $derived(!!f.firstSeen && f.firstSeen !== app.selectedSha);
  const many = $derived(members.length > 1);
  const ref = $derived(members.find((m) => m.ref)?.ref);
  const name = (p: string) => p.slice(p.lastIndexOf('/') + 1);

  function open(m: FindingWithPath, e?: MouseEvent) {
    e?.stopPropagation();
    void openFile(m.path, app.selectedSha, m.line);
  }
  function ignore(scope: IgnoreScope) {
    menu = false;
    void ignoreFinding(members, scope, team && scope !== 'commit');
  }
</script>

<svelte:window onclick={() => (menu = false)} />

<div class="finding" class:expanded class:ignored={f.ignored}>
  <button class="row" onclick={() => (expanded = !expanded)} aria-expanded={expanded}>
    <svg class="chev" width="10" height="10" viewBox="0 0 10 10" fill="currentColor"><path d="M3 1.5 7 5 3 8.5Z"/></svg>
    <span class="sev {f.severity}">{f.severity}</span>
    <span class="title">{f.title}</span>
    {#if f.ignored}<span class="tag">ignored</span>
    {:else if f.state === 'resolved'}<span class="tag good" title={f.fixedIn ? `Fixed in ${f.fixedIn.slice(0, 7)} by ${f.fixedBy}` : 'Fixed in the working tree, not committed yet'}>✓ {f.uncommitted ? 'fixed locally' : 'fixed'}</span>
    {:else if carried}<span class="tag" title="First reported in {f.firstSeen}">since {f.firstSeen?.slice(0, 7)}</span>
    {:else if f.source === 'llm'}<span class="tag" title="Found by the local model">AI</span>{/if}
    {#if many}
      <span class="tag places" title={members.map((m) => `${name(m.path)}:${m.line}`).join(', ')}>{members.length} places</span>
    {:else}
      <span class="where mono">{showPath ? name(f.path) + ':' : 'L'}{f.line}</span>
    {/if}
  </button>
  {#if expanded}
    <div class="body">
      {#if lead.detail}<p>{lead.detail}</p>{/if}
      {#if many}
        <ul class="locs">
          {#each members as m, i (i)}
            <li>
              <button onclick={(e) => open(m, e)}>
                <span class="mono">{name(m.path)}:{m.line}</span>
                {#if m.title !== f.title}<span class="faint">{m.title}</span>{/if}
                {#if m.groupRoot && mixed}<span class="root">root cause</span>{/if}
              </button>
            </li>
          {/each}
        </ul>
      {/if}
      {#if lead.suggestion}<CodeBlock code={lead.suggestion} />{/if}
      <div class="actions">
        {#if f.ignored}
          <button class="btn secondary sm" onclick={() => f.ignoreId && unignore(f.ignoreId)}>Stop ignoring</button>
        {:else if f.state === 'resolved'}
          <span class="fixed-note">
            {#if f.fixedIn}
              ✓ Fixed in <CommitRef sha={f.fixedIn} /> by <b>{f.fixedBy}</b> · {ago(f.fixedAt ?? 0)}{#if f.fixedMsg}<span class="faint"> — {f.fixedMsg}</span>{/if}
            {:else if f.uncommitted}
              ✓ Fixed in your working tree, not committed yet.
            {:else}
              ✓ The flagged code is gone; nothing to fix.
            {/if}
          </span>
        {:else}
          {#if NOT_FIXABLE.has(f.ref ?? '')}
            <button class="btn secondary sm" title="A heads-up, not a defect: confirm the readers still expect the new value" onclick={(e) => { e.stopPropagation(); void ignoreFinding(members, 'code'); }}>Mark as checked</button>
          {:else if app.status?.canFix && files.every((p) => p.endsWith('.java'))}
            <button class="btn sm" onclick={(e) => { e.stopPropagation(); void proposeFix(f); }}
              title={readiness === 'instant' ? 'Mechanical fix: opens instantly' : readiness === 'ready' ? 'Prepared in the background: opens instantly' : 'The model writes the fix (20–60 s)'}>
              {#if readiness}<svg width="11" height="11" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M9.6 1.2 3 9h4.4l-1 5.8L13 7H8.6z"/></svg>{/if}
              {files.length > 1 ? `Fix all (${files.length} files)` : 'Fix'}
            </button>
          {/if}
          {#if !many}<button class="btn secondary sm" onclick={(e) => open(f, e)}>Open in code</button>{/if}
          {#if app.status?.canFix && f.path.includes('src/main/java/') && (f.category === 'bug' || f.severity === 'critical' || f.severity === 'high')}
            <button class="btn secondary sm" title="Write a test that fails while this bug exists" onclick={(e) => { e.stopPropagation(); void openProposal('Regression test: ' + f.title, f.path, () => api.issueTest({ path: f.path, line: f.line, title: f.title, detail: f.detail })); }}>Regression test</button>
          {/if}
          <div class="menu-wrap">
            <button class="btn secondary sm" aria-haspopup="menu" aria-expanded={menu} onclick={(e) => { e.stopPropagation(); menu = !menu; }}>Ignore ▾</button>
            {#if menu}
              <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
              <div class="menu" role="menu" tabindex="-1" onclick={(e) => e.stopPropagation()}>
                <button role="menuitem" onclick={() => ignore('commit')}><b>In this commit</b><span>Hide {many ? `all ${members.length} places` : 'it'} here only</span></button>
                <button role="menuitem" onclick={() => ignore('code')}><b>On {many ? 'these lines' : 'this line'}, always</b><span>Same code in any later commit</span></button>
                <button role="menuitem" disabled={!ref} onclick={() => ignore('rule')} title={ref ? '' : 'Only findings with a rule id can be ignored everywhere'}>
                  <b>This rule everywhere</b><span>{ref ?? 'no rule id'}</span>
                </button>
                <label class="team"><input type="checkbox" bind:checked={team} /> Share with the team (.codist/ignore.yaml)</label>
              </div>
            {/if}
          </div>
        {/if}
        <span class="faint meta">{f.category}{ref ? ` · ${ref}` : ''}{f.onChangedLine ? '' : ' · outside changed lines'}</span>
      </div>
    </div>
  {/if}
</div>

<style>
  .finding { border-bottom: 1px solid var(--border); }
  .finding.ignored { opacity: 0.6; }
  .row { display: flex; align-items: center; gap: 8px; width: 100%; padding: 8px 4px; border: 0; background: none; text-align: left; }
  .row:hover { background: var(--hover); }
  .chev { flex-shrink: 0; color: var(--text-3); transition: transform 0.15s var(--ease); }
  .expanded .chev { transform: rotate(90deg); }
  .title { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fixed-note { display: inline-flex; align-items: center; flex-wrap: wrap; gap: 4px; color: var(--good); font-size: 12px; }
  .fixed-note b { color: var(--text); font-weight: 500; }
  .tag.good { color: var(--good); border-color: color-mix(in srgb, var(--good) 40%, transparent); }
  .tag { font-size: 10.5px; font-weight: 500; padding: 0 6px; border-radius: 4px; color: var(--text-2); border: 1px solid var(--border); white-space: nowrap; }
  .where { font-size: 11px; color: var(--text-3); white-space: nowrap; }
  .body { padding: 0 4px 12px 22px; }
  p { margin: 0 0 8px; color: var(--text-2); line-height: 1.55; }
  .locs { list-style: none; margin: 0 0 8px; padding: 0; border-left: 2px solid var(--border); }
  .locs button { display: flex; align-items: baseline; gap: 8px; width: 100%; padding: 3px 8px; border: 0; background: none; text-align: left; font-size: 12px; }
  .locs button:hover { background: var(--hover); }
  .locs .mono { color: var(--accent-text); font-size: 11.5px; white-space: nowrap; }
  .locs .faint { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .root { margin-left: auto; font-size: 10.5px; color: var(--text-2); white-space: nowrap; }
  .actions { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin-top: 8px; font-size: 11.5px; }
  .meta { margin-left: 4px; }
  .menu-wrap { position: relative; }
  .menu { position: absolute; z-index: 30; top: 28px; left: 0; width: 270px; padding: 4px; border: 1px solid var(--border-strong); border-radius: var(--r-lg); background: var(--surface); box-shadow: var(--shadow); }
  .menu button { display: grid; width: 100%; padding: 6px 8px; border: 0; border-radius: var(--r); background: none; text-align: left; }
  .menu button:hover:not(:disabled) { background: var(--active); }
  .menu button:disabled { opacity: 0.45; cursor: default; }
  .menu b { font-weight: 500; font-size: 12.5px; }
  .menu span { font-size: 11.5px; color: var(--text-3); }
  .team { display: flex; align-items: center; gap: 6px; padding: 6px 8px 4px; border-top: 1px solid var(--border); margin-top: 4px; color: var(--text-2); font-size: 11.5px; }
</style>
