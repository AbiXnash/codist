<script lang="ts">
  import { app, fileInCommit, openFile, selectNode } from '../lib/store.svelte';
  import { shortName, stereo } from '../lib/theme';
  import Glyph from './Glyph.svelte';
  import FindingCard from './FindingCard.svelte';

  const node = $derived(app.graph?.nodes.find((n) => n.id === app.selectedNode) ?? null);
  const out = $derived(node ? (app.graph?.edges ?? []).filter((e) => e.source === node.id) : []);
  const inc = $derived(node ? (app.graph?.edges ?? []).filter((e) => e.target === node.id) : []);
  const changed = $derived(node ? app.detail?.analysis.changed.find((c) => c.id === node.id) : undefined);
  const findings = $derived(node ? app.findings.filter((f) => f.nodeId === node.id) : []);
  let showFields = $state(false);
  const members = $derived(node ? node.members.filter((m) => showFields || m.kind !== 'field') : []);
  const fieldCount = $derived(node ? node.members.filter((m) => m.kind === 'field').length : 0);

  function go(id: string) {
    selectNode(id);
    window.dispatchEvent(new CustomEvent('codist:center', { detail: id }));
  }
  function open(line: number) {
    if (node) void openFile(node.file, fileInCommit(node.file) ? app.selectedSha : null, line);
  }
</script>

{#if !node}
  <div class="empty">Click a class in the graph.</div>
{:else}
  {@const s = stereo(node.stereotype)}
  <div class="type">
    <header>
      <div class="title"><Glyph stereotype={node.stereotype} /><h2>{node.name}</h2><span class="kind">{s.label}{node.kind !== 'class' ? ` · ${node.kind}` : ''}</span></div>
      <button class="file mono" onclick={() => open(node.startLine)} title="Open {node.file}">{node.pkg} · {node.file.split('/').pop()}:{node.startLine}</button>
    </header>

    {#if changed}
      <p class="changed"><span class="dot" style:background={changed.status === 'added' ? 'var(--added)' : 'var(--changed)'}></span>
        {changed.status === 'added' ? 'Added' : 'Modified'} in this commit{changed.members.length && changed.status !== 'added' ? `: ${changed.members.join(', ')}` : ''}</p>
    {/if}

    {#if findings.length}
      <section>
        <h3>Findings <span class="n">{findings.length}</span></h3>
        {#each findings as f, i (i)}<FindingCard {f} showPath />{/each}
      </section>
    {/if}

    {#if node.endpoints.length}
      <section>
        <h3>Endpoints</h3>
        {#each node.endpoints as e}
          {@const [verb, ...rest] = e.split(' ')}
          <div class="endpoint mono"><span class="verb {verb}">{verb}</span>{rest.join(' ')}</div>
        {/each}
      </section>
    {/if}

    <section>
      <h3>Depends on <span class="n">{out.length}</span></h3>
      <div class="chips">
        {#each out as e}<button class="chip" title="{e.target} ({e.kind})" onclick={() => go(e.target)}>{shortName(e.target)}</button>{:else}<span class="faint">—</span>{/each}
      </div>
    </section>
    <section>
      <h3>Used by <span class="n">{inc.length}</span></h3>
      <div class="chips">
        {#each inc as e}<button class="chip" title="{e.source} ({e.kind})" onclick={() => go(e.source)}>{shortName(e.source)}</button>{:else}<span class="faint">—</span>{/each}
      </div>
    </section>

    <section>
      <h3>
        Members <span class="n">{node.members.length}</span>
        {#if fieldCount}<button class="link" onclick={() => (showFields = !showFields)}>{showFields ? 'Hide' : 'Show'} fields</button>{/if}
      </h3>
      {#each members as m}
        <button class="member mono" onclick={() => open(m.startLine)} title={m.signature}>
          <span class="mk {m.kind}">{m.kind === 'field' ? 'f' : m.kind === 'constructor' ? 'c' : 'm'}</span>
          <span class="sig">{m.signature}{#if m.value}<span class="val"> = {m.value}</span>{/if}</span>
          <span class="faint">{m.startLine}</span>
        </button>
      {/each}
    </section>
  </div>
{/if}

<style>
  .type { padding: 16px; }
  .title { display: flex; align-items: center; gap: 8px; }
  h2 { margin: 0; font-size: 16px; font-weight: 600; }
  .kind { font-size: 12px; color: var(--text-3); }
  .file { display: block; margin-top: 4px; padding: 0; border: 0; background: none; font-size: 11.5px; color: var(--text-3); text-align: left; }
  .file:hover { color: var(--accent-text); }
  .changed { display: flex; align-items: center; gap: 8px; margin: 12px 0 0; font-size: 12px; color: var(--text-2); }

  section { margin-top: 18px; }
  h3 { display: flex; align-items: center; gap: 6px; margin: 0 0 6px; font-size: 12px; font-weight: 600; color: var(--text-2); }
  .n { color: var(--text-3); font-weight: 400; }
  .link { margin-left: auto; padding: 0; border: 0; background: none; font-size: 11.5px; font-weight: 400; color: var(--accent-text); }
  .chips { display: flex; flex-wrap: wrap; gap: 4px; }

  .endpoint { display: flex; align-items: center; gap: 8px; padding: 3px 0; font-size: 12px; }
  .verb { width: 50px; font-weight: 600; font-size: 11px; }
  .verb.GET { color: var(--sev-low); } .verb.POST { color: var(--added); } .verb.PUT, .verb.PATCH { color: var(--warn); } .verb.DELETE { color: var(--bad); }

  .member { display: flex; align-items: center; gap: 8px; width: 100%; padding: 4px 6px; border: 0; border-radius: var(--r); background: none; text-align: left; font-size: 12px; }
  .member:hover { background: var(--hover); }
  .mk { width: 16px; height: 16px; display: grid; place-items: center; border-radius: 3px; font-size: 10px; font-weight: 600; flex-shrink: 0; }
  .mk.method { color: #8b5cf6; background: rgba(139, 92, 246, 0.12); }
  .mk.constructor { color: var(--added); background: color-mix(in srgb, var(--added) 12%, transparent); }
  .mk.field { color: var(--sev-low); background: color-mix(in srgb, var(--sev-low) 12%, transparent); }
  .val { color: var(--text-3); }
  .sig { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
