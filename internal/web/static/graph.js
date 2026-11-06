// Draws the code graph. The server decides what is in the picture; this file only draws it.
// Click a box to see details in the side panel, double-click to open it a level deeper.
function codistGraph(module, cls) {
  const css = getComputedStyle(document.documentElement);
  const v = (name) => css.getPropertyValue(name).trim();
  const colour = {
    controller: '#0071e3', service: '#34c759', client: '#af52de', repository: '#ff9500',
    entity: '#8e8e93', component: '#5ac8fa', config: '#a2845e', other: '#8e8e93',
    method: v('--accent'), library: '#8e8e93', module: v('--accent'),
  };
  const params = new URLSearchParams();
  if (cls) params.set('class', cls); else if (module) params.set('module', module);

  fetch('/graph/data?' + params).then((r) => r.json()).then((g) => {
    document.getElementById('crumb').innerHTML = (g.crumb || [])
      .map((c, i, all) => i === all.length - 1 ? `<b>${esc(c.Label)}</b>` : `<a href="${esc(c.Href)}">${esc(c.Label)}</a>`)
      .join(' <span class="faint">/</span> ');
    document.getElementById('graph-note').textContent = g.note || '';
    htmx.process(document.getElementById('crumb'));

    const cy = cytoscape({
      container: document.getElementById('cy'),
      elements: { nodes: g.nodes || [], edges: g.edges || [] },
      wheelSensitivity: 0.25,
      style: [
        { selector: 'node', style: {
          'shape': 'round-rectangle', 'width': 'label', 'height': 'label', 'padding': '10px',
          'label': (n) => n.data('sub') ? n.data('label') + '\n' + n.data('sub') : n.data('label'),
          'text-wrap': 'wrap', 'text-valign': 'center', 'text-halign': 'center',
          'font-size': 12, 'font-family': '-apple-system, BlinkMacSystemFont, system-ui, sans-serif',
          'color': v('--text'), 'background-color': v('--surface'),
          'border-width': 2, 'border-color': (n) => (g.level === 'services' && n.data('kind') === 'service' ? colour.module : colour[n.data('kind')]) || v('--border'),
        }},
        { selector: 'node[kind = "library"]', style: { 'border-style': 'dashed' } },
        { selector: ':parent', style: {
          'background-color': v('--surface-2'), 'background-opacity': 0.6, 'border-width': 1, 'border-color': v('--border'),
          'label': 'data(label)', 'text-valign': 'top', 'text-halign': 'center', 'font-weight': 600, 'color': v('--text-2'), 'padding': '16px',
        }},
        { selector: 'edge', style: {
          'width': (e) => Math.min(1 + Math.log2(e.data('weight') || 1), 5), 'line-color': v('--text-3'),
          'target-arrow-color': v('--text-3'), 'target-arrow-shape': 'triangle', 'curve-style': 'bezier', 'opacity': 0.7,
          'label': (e) => g.level === 'services' ? '' : (e.data('label') || ''), 'font-size': 10, 'color': v('--text-2'), 'text-background-color': v('--bg'), 'text-background-opacity': 1,
        }},
        { selector: 'edge[kind = "uses"]', style: { 'line-style': 'dashed' } },
        { selector: '.faded', style: { 'opacity': 0.15 } },
        { selector: 'node:selected', style: { 'border-width': 4, 'border-color': v('--accent') } },
      ],
      layout: { name: 'preset' },
    });
    place(cy, g);
    cy.fit(undefined, 30);
    if (cy.zoom() < 0.6) { // too small to read: start readable at the top left, Fit shows the whole
      const box = cy.elements().boundingBox();
      cy.zoom(0.75);
      cy.pan({ x: 30 - box.x1 * 0.75, y: 30 - box.y1 * 0.75 });
    }

    cy.on('tap', 'node', (e) => {
      const n = e.target;
      if (n.isParent() && n.data('kind') === 'layer' && g.level !== 'class') return;
      const near = n.closedNeighborhood().union(n.ancestors());
      cy.elements().not(near).addClass('faded');
      near.removeClass('faded');
      htmx.ajax('GET', '/graph/info?id=' + encodeURIComponent(n.id()), { target: '#info' });
    });
    cy.on('tap', (e) => { if (e.target === cy) cy.elements().removeClass('faded'); });
    cy.on('dbltap', 'node', (e) => {
      const href = e.target.data('href');
      if (href) location.href = href; // a full load keeps the Back button working
    });
    document.getElementById('fit').onclick = () => cy.fit(undefined, 30);
  });

  // Fixed layouts read better than force layouts here: the picture has a meaning in each direction.
  function place(cy, g) {
    const width = (n) => Math.max(120, String(n.data('label')).length * 7.2 + 30);
    const column = (nodes, x, y0, gap) => {
      let y = y0;
      nodes.forEach((n) => { n.position({ x, y }); y += gap; });
      return y;
    };
    if (g.level === 'services') {
      // Services in a ring, libraries in the middle.
      const svc = cy.nodes().filter((n) => n.data('kind') !== 'library');
      const libs = cy.nodes().filter((n) => n.data('kind') === 'library');
      const r = Math.max(220, svc.length * 48);
      svc.forEach((n, i) => {
        const a = (2 * Math.PI * i) / svc.length - Math.PI / 2;
        n.position({ x: r * 1.5 * Math.cos(a), y: r * Math.sin(a) });
      });
      libs.forEach((n, i) => n.position({ x: (i - (libs.length - 1) / 2) * 200, y: 0 }));
      return;
    }
    if (g.level === 'module') {
      // One column block per layer, outside layers on the left; classes in a grid inside each block.
      let x = 0;
      cy.nodes(':parent').sort((a, b) => order(a) - order(b)).forEach((layer) => {
        const kids = layer.children().sort((a, b) => String(a.data('label')).localeCompare(String(b.data('label'))));
        const cols = Math.max(1, Math.ceil(kids.length / 14));
        const w = Math.max(...kids.map(width)) + 20;
        kids.forEach((n, i) => n.position({ x: x + (i % cols) * w, y: Math.floor(i / cols) * 46 }));
        x += cols * w + 90;
      });
      return;
    }
    // A class: callers on the left, its own methods in the middle, what it calls on the right.
    const own = cy.nodes().filter((n) => n.data('kind') === 'method');
    const outside = cy.nodes().filter((n) => !n.isParent() && n.data('kind') !== 'method');
    const callers = outside.filter((n) => n.outgoers('node').some((m) => m.data('kind') === 'method'));
    const callees = outside.not(callers);
    const lw = Math.max(160, ...callers.map(width)), mw = Math.max(160, ...own.map(width));
    const h = Math.max(own.length, callers.length, callees.length) * 40;
    column(callers, 0, (h - callers.length * 40) / 2, 40);
    column(own, lw + 120, (h - own.length * 40) / 2, 40);
    column(callees, lw + mw + 280, (h - callees.length * 40) / 2, 40);
  }
  function order(layer) {
    const ids = ['controller', 'service', 'client', 'repository', 'entity', 'component', 'config', 'other'];
    return ids.indexOf(String(layer.id()).slice(2));
  }
  function esc(s) {
    return String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);
  }
}
