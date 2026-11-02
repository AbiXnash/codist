<script lang="ts">
  import { app, openFile } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import type { ImpactPlace, ImpactReport } from '../lib/types';

  // What this commit can affect, measured from the code.
  let { sha }: { sha: string } = $props();
  let rep = $state.raw<ImpactReport | null>(null);
  let failed = $state(false);
  $effect(() => {
    const at = sha;
    failed = false;
    api.impact(at).then((r) => { if (at === sha) rep = r; }, () => { if (at === sha) failed = true; });
  });
  const go = (p: { file: string; line: number }) => {
    app.showPanel = true;
    void openFile(p.file, sha, p.line || null);
  };
  const LABEL: Record<string, string> = { added: 'New', modified: 'Changed', signature: 'Signature changed', removed: 'Removed' };
  const label = (p: ImpactPlace) => `${p.class}.${p.method}`;
</script>

{#if rep && rep.changed.length}
  <section class="card impact">
    <div class="head"><span class="label">What this change can affect</span></div>
    <p class="sum">
      {rep.changed.length} method{rep.changed.length === 1 ? '' : 's'} changed, reaching {rep.callerCount} other method{rep.callerCount === 1 ? '' : 's'} in {rep.classes} class{rep.classes === 1 ? '' : 'es'},
      {rep.routes.length} API route{rep.routes.length === 1 ? '' : 's'} and {rep.tests.length} test{rep.tests.length === 1 ? '' : 's'}.
    </p>
    {#if rep.untestedRisk}<p class="warn">No test reaches {rep.untestedRisk} of the existing methods changed here.</p>{/if}

    <div class="sub">Changed</div>
    {#each rep.changed as c}
      <button class="row" disabled={!c.line} onclick={() => go(c)}>
        <span class="mono t">{c.class}.{c.method}</span>
        <span class="st" class:hot={c.status === 'signature' || c.status === 'removed'}>{LABEL[c.status]}</span>
        <span class="faint n">{c.callers} caller{c.callers === 1 ? '' : 's'}, {c.tests} test{c.tests === 1 ? '' : 's'}</span>
      </button>
    {/each}

    {#if rep.routes.length}
      <div class="sub">API routes</div>
      {#each rep.routes as r}<button class="row" onclick={() => go(r)}><span class="mono t">{r.route}</span><span class="faint n">{r.class}.{r.method}</span></button>{/each}
    {/if}

    {#if rep.callers.length}
      <details>
        <summary>Callers ({rep.callerCount}{rep.possible ? `, ${rep.possible} possible` : ''})</summary>
        {#each rep.callers as p}
          <button class="row" onclick={() => go(p)}><span class="mono t">{label(p)}</span>{#if !p.confirmed}<span class="st" title="Matched by method name only; check it">Possible</span>{/if}<span class="faint n">{p.depth} call{p.depth === 1 ? '' : 's'} away</span></button>
        {/each}
      </details>
    {/if}
    {#if rep.tests.length}
      <details>
        <summary>Tests to run ({rep.tests.length})</summary>
        {#each rep.tests as p}<button class="row" onclick={() => go(p)}><span class="mono t">{label(p)}</span></button>{/each}
      </details>
    {/if}
    <p class="faint note">From method calls in the code. It cannot see reflection, events or configuration, so something may still be missing.</p>
  </section>
{:else if failed}
  <p class="faint note">Could not work out what this change affects.</p>
{/if}

<style>
  .impact { margin-top: 14px; padding: 12px 14px; border-radius: var(--r-lg); background: var(--surface-2); }
  .head { margin-bottom: 3px; }
  .sum { margin: 0; font-size: 12.5px; color: var(--text-2); }
  .warn { margin: 6px 0 0; font-size: 12.5px; color: var(--warn); }
  .sub { margin: 10px 0 3px; font-size: 12px; font-weight: 600; color: var(--text-2); }
  .row { display: flex; align-items: center; gap: 8px; width: 100%; padding: 4px 6px; border: 0; border-radius: var(--r); background: none; text-align: left; font-size: 12px; }
  .row:not(:disabled):hover { background: var(--hover); }
  .row:disabled { cursor: default; }
  .t { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11.5px; }
  .st { flex-shrink: 0; padding: 0 6px; line-height: 16px; border-radius: 4px; font-size: 10.5px; background: var(--surface-3); color: var(--text-2); }
  .st.hot { color: var(--warn); background: color-mix(in srgb, var(--warn) 15%, transparent); }
  .n { margin-left: auto; flex-shrink: 0; font-size: 11px; }
  details { margin-top: 8px; }
  summary { cursor: pointer; font-size: 12px; font-weight: 600; color: var(--text-2); }
  .note { margin: 8px 0 0; font-size: 11.5px; }
</style>
