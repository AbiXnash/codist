<script lang="ts">
  import { tick } from 'svelte';
  import { app, askQuestion, askTarget, requestEdit } from '../lib/store.svelte';
  import CodeBlock from './CodeBlock.svelte';

  let input = $state('');
  let log = $state<HTMLDivElement>();
  const target = $derived(askTarget());
  const examples = [
    'Explain what this class does and who calls it',
    'Is there a race condition here?',
    'Add retry with exponential backoff to the external call',
    'Replace System.out with SLF4J logging',
  ];

  async function send(mode: 'ask' | 'edit') {
    const text = input.trim();
    if (!text || app.asking) return;
    input = '';
    if (mode === 'edit') await requestEdit(text);
    else await askQuestion(text);
    await tick();
    log?.scrollTo({ top: log.scrollHeight, behavior: 'smooth' });
  }

  function key(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      void send(e.metaKey || e.ctrlKey ? 'edit' : 'ask');
    }
  }

  /** Splits an answer into prose and fenced code blocks. */
  function parts(text: string): { code: boolean; text: string; lang: string }[] {
    const out: { code: boolean; text: string; lang: string }[] = [];
    const re = /```(\w*)\n([\s\S]*?)```/g;
    let last = 0;
    for (const m of text.matchAll(re)) {
      if (m.index! > last) out.push({ code: false, text: text.slice(last, m.index), lang: '' });
      out.push({ code: true, text: m[2].replace(/\n$/, ''), lang: m[1] || 'java' });
      last = m.index! + m[0].length;
    }
    if (last < text.length) out.push({ code: false, text: text.slice(last), lang: '' });
    return out;
  }
</script>

<div class="ask">
  <div class="context">
    <span class="label">Context</span>
    {#if target.path}<span class="mono">{target.path.split('/').pop()}</span>{:else}<span class="faint">whole project — open a file or click a class to focus</span>{/if}
    <span class="faint models">answers: {app.status?.models?.ask} · edits: {app.status?.models?.fix}</span>
  </div>

  <div class="log" bind:this={log}>
    {#each app.ask as m, i (i)}
      <div class="msg {m.role}" class:error={m.error}>
        {#if m.role === 'user'}
          <p>{m.text}</p>
        {:else}
          {#each parts(m.text) as p}
            {#if p.code}<CodeBlock code={p.text} lang={p.lang} />{:else}<p>{p.text.trim()}</p>{/if}
          {/each}
          {#if !m.error && m.path}
            <button class="btn secondary sm" onclick={() => requestEdit(app.ask[i - 1]?.text ?? '')} title="Apply what was asked as a reviewed commit">Make this change</button>
          {/if}
        {/if}
      </div>
    {:else}
      <div class="examples">
        <p class="faint">Ask about the code, or describe a change. Changes open as a diff you approve before anything is committed.</p>
        {#each examples as ex}<button class="chip" onclick={() => (input = ex)}>{ex}</button>{/each}
      </div>
    {/each}
    {#if app.asking}<div class="msg assistant"><span class="spinner"></span> Thinking…</div>{/if}
  </div>

  <div class="composer">
    <textarea bind:value={input} onkeydown={key} rows="3" placeholder="Ask a question, or describe a change…" spellcheck="false"></textarea>
    <div class="buttons">
      <span class="faint hint">↵ ask · ⌘↵ change code</span>
      <button class="btn secondary sm" disabled={!input.trim() || app.asking} onclick={() => send('ask')}>Ask</button>
      <button class="btn sm" disabled={!input.trim() || !target.path || !app.status?.canFix} onclick={() => send('edit')} title={target.path ? 'Propose a diff for this file' : 'Open a file first'}>Change code</button>
    </div>
  </div>
</div>

<style>
  .ask { display: flex; flex-direction: column; height: 100%; }
  .context { display: flex; align-items: center; gap: 8px; padding: 8px 14px; border-bottom: 1px solid var(--border); font-size: 12px; flex-shrink: 0; }
  .models { margin-left: auto; font-size: 11px; }
  .log { flex: 1; overflow: auto; padding: 12px 14px; display: flex; flex-direction: column; gap: 10px; }
  .msg { max-width: 92%; line-height: 1.55; }
  .msg p { margin: 0 0 6px; white-space: pre-wrap; }
  .msg.user { align-self: flex-end; padding: 8px 12px; border-radius: var(--r-lg); background: var(--accent-soft); }
  .msg.assistant { align-self: flex-start; display: grid; gap: 6px; }
  .msg.error p { color: var(--bad); }
  .msg .btn { justify-self: start; }
  .examples { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; }
  .examples .chip { font-family: var(--font); }
  .composer { border-top: 1px solid var(--border); padding: 10px 12px; flex-shrink: 0; }
  textarea {
    width: 100%;
    resize: none;
    padding: 8px 10px;
    border: 1px solid var(--border-strong);
    border-radius: var(--r);
    background: var(--surface-2);
    color: var(--text);
    font: inherit;
  }
  textarea:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
  .buttons { display: flex; align-items: center; justify-content: flex-end; gap: 6px; margin-top: 6px; }
  .hint { margin-right: auto; font-size: 11.5px; }
  .btn:disabled { opacity: 0.5; cursor: default; filter: none; }
</style>
