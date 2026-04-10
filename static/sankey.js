// ─── Layout constants (viewBox coordinate space) ────────────────────────────
const VB_W = 900;   // viewBox width
const VB_H = 480;   // viewBox height

// Margins: left/top are small; right is wide to give leaf labels breathing room.
const M = { top: 12, right: 195, bottom: 12, left: 12 };

// The actual sankey layout fits inside the margins.
const LAYOUT_X0 = M.left;
const LAYOUT_X1 = VB_W - M.right;   // 705 — rightmost node column ends here
const LAYOUT_Y0 = M.top;
const LAYOUT_Y1 = VB_H - M.bottom;

const NODE_WIDTH = 16;
const MAX_NODE_PADDING = 16;
const MIN_NODE_PADDING = 4;

// ─── Helpers ─────────────────────────────────────────────────────────────────

function truncate(str, max) {
  return str.length > max ? str.slice(0, max - 1) + '…' : str;
}

// Dynamically scale node padding so the layout never overflows vertically.
function nodePadding(nodeCount) {
  const available = LAYOUT_Y1 - LAYOUT_Y0;
  const dynamic = Math.floor(available / Math.max(1, nodeCount) / 2);
  return Math.min(MAX_NODE_PADDING, Math.max(MIN_NODE_PADDING, dynamic));
}

// ─── Render ──────────────────────────────────────────────────────────────────

async function render() {
  let data;
  try {
    const res = await fetch('/api/sankey-data');
    data = await res.json();
  } catch (e) {
    return;
  }

  const svg = d3.select('#sankey-svg');
  svg.selectAll('*').remove();

  if (!data.nodes || data.nodes.length < 2 || !data.links || data.links.length === 0) {
    svg.append('text')
      .attr('x', VB_W / 2)
      .attr('y', VB_H / 2)
      .attr('text-anchor', 'middle')
      .attr('fill', '#9ca3af')
      .style('font-size', '13px')
      .style('font-family', 'sans-serif')
      .text('Add expense categories to see the diagram');
    return;
  }

  const currency = data.currency || '£';

  const sankey = d3.sankey()
    .nodeWidth(NODE_WIDTH)
    .nodePadding(nodePadding(data.nodes.length))
    // Place every node at its natural tree depth so sub-category nodes are
    // never pushed into the same column as unrelated leaves (sankeyJustify
    // default would do that).
    .nodeAlign(d3.sankeyLeft)
    // Sort nodes within each column by their BFS input index.  Siblings from
    // the same parent have consecutive indices, so the relaxation starts with
    // them already adjacent and naturally keeps them that way as it converges.
    .nodeSort((a, b) => a.index - b.index)
    .extent([[LAYOUT_X0, LAYOUT_Y0], [LAYOUT_X1, LAYOUT_Y1]]);

  const { nodes, links } = sankey({
    nodes: data.nodes.map(d => Object.assign({}, d)),
    links: data.links.map(d => Object.assign({}, d)),
  });

  const color = d3.scaleOrdinal(d3.schemeTableau10);
  const isUnallocated = d => d.name === 'Unallocated';

  // ── Links ──────────────────────────────────────────────────────────────────
  svg.append('g')
    .attr('fill', 'none')
    .selectAll('path')
    .data(links)
    .join('path')
    .attr('d', d3.sankeyLinkHorizontal())
    .attr('stroke', d => isUnallocated(d.target) ? '#d1d5db' : color(d.source.name))
    .attr('stroke-width', d => Math.max(1, d.width))
    .attr('opacity', 0.4);

  // ── Nodes ──────────────────────────────────────────────────────────────────
  const node = svg.append('g')
    .selectAll('g')
    .data(nodes)
    .join('g');

  node.append('rect')
    .attr('x', d => d.x0)
    .attr('y', d => d.y0)
    .attr('height', d => Math.max(1, d.y1 - d.y0))
    .attr('width', d => d.x1 - d.x0)
    .attr('fill', d => isUnallocated(d) ? '#e5e7eb' : color(d.name))
    .attr('rx', 3);

  // ── Labels ─────────────────────────────────────────────────────────────────
  // Root (income): label to the right, reading into the flow.
  // All other nodes: label to the left of their block (end-anchored at x0-6).
  node.append('text')
    .attr('x', d => d.isRoot ? d.x1 + 6 : d.x0 - 6)
    .attr('y', d => (d.y1 + d.y0) / 2)
    .attr('dy', '0.35em')
    .attr('text-anchor', d => d.isRoot ? 'start' : 'end')
    .style('font-size', '11px')
    .style('font-family', 'sans-serif')
    .style('fill', d => isUnallocated(d) ? '#9ca3af' : '#374151')
    .text(d => `${truncate(d.name, 15)} (${currency}${d.value.toFixed(2)})`);
}

// Initial render on page load
render();

// Re-render after every HTMX swap
document.body.addEventListener('htmx:afterSettle', render);
