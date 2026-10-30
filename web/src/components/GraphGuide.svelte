<script lang="ts">
  import { app } from '../lib/store.svelte';
  import { SEVERITY_COLOR, STATE_COLOR, STEREOTYPE } from '../lib/theme';
  import Glyph from './Glyph.svelte';

  // Shapes drawn as small SVGs, matching the graph's node shapes.
  const shapes: Record<string, string> = {
    'round-rectangle': '<rect x="3" y="5" width="18" height="14" rx="4"/>',
    ellipse: '<circle cx="12" cy="12" r="8"/>',
    barrel: '<path d="M5 6q7-3 14 0v12q-7 3-14 0z"/>',
    'cut-rectangle': '<path d="M7 5h10l3 3v8l-3 3H7l-3-3V8z"/>',
    'round-tag': '<path d="M4 6h11l5 6-5 6H4z"/>',
    hexagon: '<path d="M7 4h10l4 8-4 8H7l-4-8z"/>',
    octagon: '<path d="M8 3h8l5 5v8l-5 5H8l-5-5V8z"/>',
    'round-triangle': '<path d="M12 4l9 15H3z"/>',
    triangle: '<path d="M12 4l9 15H3z"/>',
  };
  const roles = Object.entries(STEREOTYPE);
  const roleText: Record<string, string> = {
    controller: '@RestController / @Controller: HTTP endpoints',
    service: '@Service: business logic',
    repository: '@Repository or a Spring Data interface: database access',
    entity: '@Entity: a database table',
    dto: 'Records and request/response classes that carry data',
    config: '@Configuration and @ConfigurationProperties',
    component: 'Other Spring beans (@Component)',
    advice: '@ControllerAdvice: error handling for controllers',
    exception: 'Exception classes',
    other: 'Plain classes and interfaces',
    test: 'Test classes (shown with "Tests")',
  };
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="scrim" onclick={() => (app.showGuide = false)}>
  <div class="guide" role="dialog" aria-label="How to read the graph" tabindex="-1" onclick={(e) => e.stopPropagation()}>
    <header>
      <h2>How to read the graph</h2>
      <button class="x" aria-label="Close" onclick={() => (app.showGuide = false)}>×</button>
    </header>
    <div class="cols">
      <section>
        <h3>Nodes are classes. Shape and letter say what role it plays</h3>
        {#each roles as [k, s]}
          <div class="r">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="var(--node-fill)" stroke="var(--node-border)" stroke-width="1.6">{@html shapes[s.shape] ?? shapes.ellipse}</svg>
            <Glyph stereotype={k} /><b>{s.label}</b><span class="faint">{roleText[k] ?? ''}</span>
          </div>
        {/each}
        <div class="r"><svg width="24" height="24" viewBox="0 0 24 24" fill="var(--node-fill)" stroke="var(--node-border)" stroke-width="1.6" stroke-dasharray="3 2"><circle cx="12" cy="12" r="8"/></svg><b>Dashed border</b><span class="faint">interface or abstract class</span></div>
        <div class="r"><svg width="24" height="24" viewBox="0 0 24 24" fill="var(--node-fill)" stroke="var(--node-border)"><circle cx="8" cy="12" r="4"/><circle cx="17" cy="12" r="6.5"/></svg><b>Size</b><span class="faint">number of links by default; Controls → Size by switches to code size or open issues</span></div>
      </section>

      <section>
        <h3>Lines are dependencies. A → B means A uses B</h3>
        <div class="r"><svg width="44" height="14"><line x1="2" y1="7" x2="34" y2="7" stroke="var(--text-2)" stroke-width="2.5"/><path d="M34 3l8 4-8 4z" fill="var(--text-2)"/></svg><b>Thick: injects</b><span class="faint">a field or constructor dependency Spring fills in</span></div>
        <div class="r"><svg width="44" height="14"><line x1="2" y1="7" x2="34" y2="7" stroke="var(--text-2)" stroke-width="1"/><path d="M34 4l7 3-7 3z" fill="var(--text-2)"/></svg><b>Thin: uses</b><span class="faint">calls it, takes or returns it, creates it</span></div>
        <div class="r"><svg width="44" height="14"><line x1="2" y1="7" x2="33" y2="7" stroke="var(--text-2)"/><path d="M33 2l9 5-9 5z" fill="none" stroke="var(--text-2)"/></svg><b>Hollow arrow: extends</b><span class="faint">dashed when it implements an interface</span></div>

        <h3>Rings are state: what the selected commit or the issue list says</h3>
        <div class="r"><i class="ring" style:border-color={STATE_COLOR.changed}></i><b>Changed</b><span class="faint">modified in the selected commit (or in your uncommitted edits)</span></div>
        <div class="r"><i class="ring" style:border-color={STATE_COLOR.added}></i><b>Added</b><span class="faint">new in the selected commit</span></div>
        <div class="r"><i class="ring dashed" style:border-color={STATE_COLOR.impacted}></i><b>Impacted</b><span class="faint">not changed, but depends on something that was; orange lines show why</span></div>
        <div class="r">
          <span class="rings">{#each ['critical', 'high', 'medium', 'low'] as s}<i class="ring glow" style:border-color={SEVERITY_COLOR[s as 'critical']} title={s}></i>{/each}</span>
          <b>Issue glow</b><span class="faint">open issues in the class, coloured by the worst: critical, high, medium, low. Follows the Issues panel's filters</span>
        </div>
        <div class="r"><i class="ring" style:border-color="var(--accent)"></i><b>Selected</b><span class="faint">the class you clicked; its links turn blue</span></div>
        <div class="r"><span class="dimmed">Aa</span><b>Faded</b><span class="faint">not relevant to the current focus, search or analysis</span></div>
      </section>

      <section>
        <h3>Analysis overlays (Controls → Analysis)</h3>
        <div class="r"><i class="ring" style:border-color="#e11d48"></i><b>Red: dependency cycle</b><span class="faint">classes that depend on each other in a loop; none can change or be tested alone</span></div>
        <div class="r"><svg width="44" height="14"><line x1="2" y1="7" x2="40" y2="7" stroke="#a855f7" stroke-width="2.5" stroke-dasharray="5 3"/></svg><b>Purple dashed: layer violation</b><span class="faint">a repository depending on a service, an entity on a repository, or a controller skipping the service</span></div>
        <div class="r"><i class="ring glow" style:border-color="#f97316"></i><b>Orange: blast radius</b><span class="faint">what depends on the chosen class; darker = closer. Re-test these after a change</span></div>
        <div class="r"><i class="ring" style:border-color="#06b6d4"></i><b>Cyan: path</b><span class="faint">how one class reaches another</span></div>

        <h3>Numbers (hover a class)</h3>
        <dl>
          <dt>in</dt><dd>how many classes depend on it. High = central; a change ripples.</dd>
          <dt>out</dt><dd>how many classes it depends on. High = knows too much, hard to test.</dd>
          <dt>instability</dt><dd>out ÷ (in + out). 0 = stable foundation, 1 = leaf that nothing relies on. Classes many depend on should be stable.</dd>
          <dt>lines</dt><dd>size of the class.</dd>
        </dl>

        <h3>Mouse and keys</h3>
        <dl>
          <dt>click</dt><dd>open the class (details, code)</dd>
          <dt>right-click</dt><dd>blast radius, paths, show only neighbours or dependencies, hide</dd>
          <dt>drag</dt><dd>move a class; the rest settles around it</dd>
          <dt>/ or ⌘K</dt><dd>go to a class</dd>
        </dl>
      </section>
    </div>
  </div>
</div>

<style>
  .scrim { position: absolute; inset: 0; z-index: 8; display: grid; place-items: center; padding: 20px; background: color-mix(in srgb, var(--bg) 55%, transparent); }
  .guide { width: min(1100px, 100%); max-height: 100%; overflow: auto; border: 1px solid var(--border-strong); border-radius: 12px; background: var(--surface); box-shadow: var(--shadow); }
  header { position: sticky; top: 0; display: flex; align-items: center; padding: 14px 18px; border-bottom: 1px solid var(--border); background: var(--surface); }
  h2 { margin: 0; font-size: 15px; }
  .x { margin-left: auto; border: 0; background: none; font-size: 18px; color: var(--text-3); }
  .cols { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 22px; padding: 16px 18px 20px; }
  @media (max-width: 1000px) { .cols { grid-template-columns: 1fr; } }
  h3 { margin: 12px 0 8px; font-size: 11.5px; font-weight: 600; color: var(--text-2); text-transform: uppercase; letter-spacing: 0.04em; line-height: 1.4; }
  section > h3:first-child { margin-top: 0; }
  .r { display: grid; grid-template-columns: auto auto 1fr; align-items: center; gap: 4px 8px; padding: 4px 0; font-size: 12.5px; }
  .r .faint { grid-column: 1 / -1; padding-left: 0; font-size: 11.5px; margin-top: -2px; }
  .r > b { font-weight: 600; }
  .ring { display: inline-block; width: 14px; height: 14px; border-radius: 50%; border: 2.5px solid; }
  .ring.dashed { border-style: dashed; }
  .ring.glow { box-shadow: 0 0 0 3px color-mix(in srgb, currentColor 0%, transparent); background: color-mix(in srgb, var(--surface) 70%, transparent); }
  .rings { display: inline-flex; gap: 3px; }
  .dimmed { opacity: 0.25; font-weight: 700; }
  dl { display: grid; grid-template-columns: auto 1fr; gap: 4px 10px; margin: 0; font-size: 12px; }
  dt { font-family: var(--mono); font-size: 11.5px; color: var(--accent-text); }
  dd { margin: 0; color: var(--text-2); }
</style>
