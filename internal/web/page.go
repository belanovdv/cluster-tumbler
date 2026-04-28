// Package web содержит embedded HTML UI и static assets.
//
// UI является draft-представлением текущего состояния cluster-tumbler.
// Управление через UI пока не реализовано.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets/*
var assetsFS embed.FS

// AssetsHandler отдает embedded SVG/CSS/JS assets по /assets/.
func AssetsHandler() http.Handler {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err)
	}

	return http.StripPrefix("/assets/", http.FileServer(http.FS(sub)))
}

// Handler отдает главную HTML-страницу.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(pageHTML))
	}
}

const pageHTML = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>PT Cluster Tumblers</title>

<style>
:root {
  --line:#e1e5ee;
  --muted:#66708a;
  --text:#20242c;
  --bg:#ffffff;
  --soft:#f7f8fb;
  --selected:#eef1f7;

  --idle:#e5e7eb;
  --active:#d8f5df;
  --passive:#fff0c2;
  --starting:#dbeafe;
  --stopping:#dbeafe;
  --failed:#ffd0d0;

  --ok:#c9f2d3;
  --warning:#ffe08a;

  --online:#23b26d;
  --offline:#dc2626;
}

* {
  box-sizing:border-box;
}

body {
  margin:0;
  font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Arial,sans-serif;
  background:var(--bg);
  color:var(--text);
  font-size:15px;
}

.layout {
  display:grid;
  grid-template-columns:56px 1fr;
  min-height:100vh;
}

.rail {
  border-right:1px solid var(--line);
  display:flex;
  flex-direction:column;
  align-items:center;
  padding:18px 0;
  gap:18px;
}

.logo {
  width:38px;
  height:38px;
  display:grid;
  place-items:center;
}

.logo img {
  width:38px;
  height:38px;
  display:block;
}

.nav-icon {
  width:40px;
  height:40px;
  border-radius:10px;
  display:grid;
  place-items:center;
}

.nav-icon.active {
  background:var(--selected);
}

.nav-icon img {
  width:22px;
  height:22px;
  display:block;
}

.main {
  padding:30px;
  overflow:hidden;
}

.title {
  font-size:26px;
  font-weight:700;
  margin-bottom:20px;
}

.meta {
  display:flex;
  gap:10px;
  margin-bottom:20px;
  color:var(--muted);
  flex-wrap:wrap;
}

.pill {
  border:1px solid #c8d0df;
  border-radius:8px;
  padding:7px 11px;
  background:#fff;
  font-size:14px;
}

.content {
  display:grid;
  grid-template-columns:340px minmax(420px,1fr) 340px;
  gap:22px;
  height:calc(100vh - 132px);
  min-height:560px;
}

.panel {
  border:1px solid var(--line);
  border-radius:14px;
  background:#fff;
  overflow:hidden;
  min-height:0;
}

.panel-header {
  padding:14px 16px;
  border-bottom:1px solid var(--line);
  font-weight:700;
  display:flex;
  justify-content:space-between;
  align-items:center;
}

.panel-body {
  padding:12px;
  height:calc(100% - 50px);
  overflow:auto;
}

.group-title {
  font-size:12px;
  text-transform:uppercase;
  letter-spacing:.04em;
  color:var(--muted);
  margin:12px 6px 8px;
  font-weight:700;
}

.mg-item {
  padding:11px;
  border-radius:10px;
  cursor:pointer;
  border:1px solid transparent;
  margin-bottom:8px;
}

.mg-item:hover {
  background:#f5f7fb;
}

.mg-item.selected {
  background:var(--selected);
  border-color:#c8d0df;
}

.mg-name {
  font-weight:700;
  margin-bottom:7px;
}

.badges {
  display:flex;
  flex-wrap:wrap;
  gap:5px;
}

.badge {
  display:inline-flex;
  align-items:center;
  padding:3px 8px;
  border-radius:999px;
  font-size:12px;
  border:1px solid rgba(0,0,0,.06);
  color:#394150;
}

.state-idle { background:var(--idle); }
.state-active { background:var(--active); }
.state-passive { background:var(--passive); }
.state-starting { background:var(--starting); }
.state-stopping { background:var(--stopping); }
.state-failed { background:var(--failed); }

.health-ok { background:var(--ok); }
.health-warning { background:var(--warning); }
.health-failed { background:var(--failed); }

.details-title {
  font-size:21px;
  font-weight:700;
  margin:0 0 14px;
}

.section {
  margin-bottom:22px;
}

.section-title {
  color:var(--muted);
  font-size:12px;
  text-transform:uppercase;
  letter-spacing:.04em;
  font-weight:700;
  margin-bottom:9px;
}

.kv {
  display:grid;
  grid-template-columns:105px 1fr;
  gap:10px;
  border-bottom:1px solid #f0f2f6;
  padding:8px 0;
}

.kv-key {
  color:var(--muted);
}

.node-card {
  border:1px solid var(--line);
  border-radius:11px;
  margin-bottom:12px;
  overflow:hidden;
}

.node-header {
  padding:10px 12px;
  background:var(--soft);
  font-weight:700;
  border-bottom:1px solid var(--line);
}

.role-row {
  display:grid;
  grid-template-columns:145px 1fr;
  gap:10px;
  padding:10px 12px;
  border-bottom:1px solid #f0f2f6;
}

.role-row:last-child {
  border-bottom:none;
}

.role-name {
  font-weight:600;
}

.agent-card {
  border:1px solid var(--line);
  border-radius:11px;
  padding:12px;
  margin-bottom:10px;
}

.agent-head {
  display:flex;
  align-items:center;
  justify-content:space-between;
  gap:10px;
  margin-bottom:9px;
}

.agent-name {
  font-weight:700;
}

.status-dot {
  width:10px;
  height:10px;
  border-radius:999px;
  flex:0 0 auto;
}

.status-dot.online {
  background:var(--online);
  box-shadow:0 0 0 3px rgba(35,178,109,.12);
}

.status-dot.offline {
  background:var(--offline);
  box-shadow:0 0 0 3px rgba(220,38,38,.12);
}

.agent-membership {
  color:var(--muted);
  font-size:13px;
  padding-top:6px;
  border-top:1px solid #f0f2f6;
  margin-top:6px;
}

.empty {
  color:var(--muted);
  padding:18px;
}

.muted {
  color:var(--muted);
}

pre {
  margin:0;
  padding:9px;
  background:var(--soft);
  border-radius:8px;
  overflow:auto;
  font-size:12px;
  line-height:1.35;
}
</style>
</head>

<body>
<div class="layout">
  <aside class="rail">
    <div class="logo" title="PT">
      <img src="/assets/brand-icon.svg" alt="PT"/>
    </div>

    <div class="nav-icon active" title="View">
      <img src="/assets/node-tree_16.svg" alt="View"/>
    </div>
  </aside>

  <main class="main">
    <div class="title">PT Cluster Tumblers</div>

    <div class="meta">
      <span class="pill" id="cluster"></span>
      <span class="pill" id="revision"></span>
      <span class="pill" id="leader"></span>
      <span class="pill" id="ready"></span>
    </div>

    <div class="content">
      <section class="panel">
        <div class="panel-header">Groups</div>
        <div class="panel-body" id="groups"></div>
      </section>

      <section class="panel">
        <div class="panel-header">Management Group</div>
        <div class="panel-body" id="details"></div>
      </section>

      <section class="panel">
        <div class="panel-header">Agent Registry</div>
        <div class="panel-body" id="registry"></div>
      </section>
    </div>
  </main>
</div>

<script>
let state = null;
let selected = null;

function escapeHtml(s) {
  return String(s)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;');
}

function badge(label, value, kind) {
  const safe = value || "unknown";
  return '<span class="badge ' + kind + '-' + safe + '">' + label + ': ' + escapeHtml(safe) + '</span>';
}

function docState(doc) {
  return doc && doc.state ? doc.state : "";
}

function docStatus(doc) {
  return doc && doc.status ? doc.status : "";
}

function jsonBlock(obj) {
  if (!obj) {
    return '<span class="muted">—</span>';
  }
  return '<pre>' + escapeHtml(JSON.stringify(obj, null, 2)) + '</pre>';
}

function loadDefaultSelection() {
  const groups = state.cluster.groups || {};
  for (const groupName of Object.keys(groups)) {
    const mgs = groups[groupName].management_groups || {};
    for (const mgName of Object.keys(mgs)) {
      selected = { groupName, mgName };
      return;
    }
  }
}

function renderMeta() {
  document.getElementById("cluster").textContent = "Cluster: " + (state.cluster.id || "—");
  document.getElementById("revision").textContent = "Revision: " + state.revision;
  document.getElementById("ready").textContent = state.ready ? "Ready" : "Not ready";

  const leader = state.cluster.leadership;
  document.getElementById("leader").textContent = "Leader: " + (leader && leader.owner_node_id ? leader.owner_node_id : "—");
}

function renderGroups() {
  const el = document.getElementById("groups");
  const groups = state.cluster.groups || {};
  let html = "";

  for (const groupName of Object.keys(groups)) {
    html += '<div class="group-title">' + escapeHtml(groupName) + '</div>';

    const mgs = groups[groupName].management_groups || {};
    for (const mgName of Object.keys(mgs)) {
      const mg = mgs[mgName];
      const isSelected = selected && selected.groupName === groupName && selected.mgName === mgName;

      html += '<div class="mg-item ' + (isSelected ? 'selected' : '') + '" onclick="selectMG(\'' + escapeHtml(groupName) + '\', \'' + escapeHtml(mgName) + '\')">';
      html += '<div class="mg-name">' + escapeHtml(mgName) + '</div>';
      html += '<div class="badges">';
      html += badge("desired", docState(mg.desired), "state");
      html += badge("actual", docState(mg.actual), "state");
      html += badge("health", docStatus(mg.health), "health");
      if (mg.config && mg.config.priority) {
        html += '<span class="badge">priority: ' + escapeHtml(mg.config.priority) + '</span>';
      }
      html += '</div>';
      html += '</div>';
    }
  }

  el.innerHTML = html || '<div class="empty">No groups</div>';
}

window.selectMG = function(groupName, mgName) {
  selected = { groupName, mgName };
  renderGroups();
  renderDetails();
};

function renderDetails() {
  const el = document.getElementById("details");

  if (!selected) {
    el.innerHTML = '<div class="empty">No management group selected</div>';
    return;
  }

  const mg = state.cluster.groups[selected.groupName].management_groups[selected.mgName];

  let html = '<div class="details-title">' + escapeHtml(selected.groupName) + ' / ' + escapeHtml(selected.mgName) + '</div>';

  html += '<div class="section">';
  html += '<div class="section-title">Summary</div>';
  html += '<div class="kv"><div class="kv-key">Config</div><div>' + jsonBlock(mg.config) + '</div></div>';
  html += '<div class="kv"><div class="kv-key">Desired</div><div>' + jsonBlock(mg.desired) + '</div></div>';
  html += '<div class="kv"><div class="kv-key">Actual</div><div>' + jsonBlock(mg.actual) + '</div></div>';
  html += '<div class="kv"><div class="kv-key">Health</div><div>' + jsonBlock(mg.health) + '</div></div>';
  html += '</div>';

  html += '<div class="section">';
  html += '<div class="section-title">Nodes / Roles</div>';

  const nodes = mg.nodes || {};
  const nodeNames = Object.keys(nodes);

  if (nodeNames.length === 0) {
    html += '<div class="empty">No nodes</div>';
  }

  for (const nodeName of nodeNames) {
    const node = nodes[nodeName];

    html += '<div class="node-card">';
    html += '<div class="node-header">' + escapeHtml(nodeName) + '</div>';

    const roles = node.roles || {};
    const roleNames = Object.keys(roles);

    if (roleNames.length === 0) {
      html += '<div class="empty">No roles</div>';
    }

    for (const roleName of roleNames) {
      const role = roles[roleName];

      html += '<div class="role-row">';
      html += '<div class="role-name">' + escapeHtml(roleName) + '</div>';
      html += '<div class="badges">';
      html += badge("actual", docState(role.actual), "state");
      html += badge("health", docStatus(role.health), "health");
      html += '</div>';
      html += '</div>';
    }

    html += '</div>';
  }

  html += '</div>';

  el.innerHTML = html;
}

function renderRegistry() {
  const el = document.getElementById("registry");
  const registry = state.cluster.registry || {};
  const sessions = state.cluster.session || {};
  const nodeNames = Object.keys(registry);

  if (nodeNames.length === 0) {
    el.innerHTML = '<div class="empty">No registered agents</div>';
    return;
  }

  let html = "";

  for (const nodeName of nodeNames) {
    const reg = registry[nodeName];
    const online = !!sessions[nodeName];

    html += '<div class="agent-card">';
    html += '<div class="agent-head">';
    html += '<div class="agent-name">' + escapeHtml(nodeName) + '</div>';
    html += '<div class="status-dot ' + (online ? 'online' : 'offline') + '" title="' + (online ? 'online' : 'offline') + '"></div>';
    html += '</div>';

    if (reg && reg.memberships) {
      for (const m of reg.memberships) {
        html += '<div class="agent-membership">';
        html += escapeHtml(m.cluster_group) + ' / ' + escapeHtml(m.management_group);
        if (m.priority) {
          html += ' · priority ' + escapeHtml(m.priority);
        }
        if (m.roles && m.roles.length) {
          html += '<br/>roles: ' + escapeHtml(m.roles.join(', '));
        }
        html += '</div>';
      }
    }

    html += '</div>';
  }

  el.innerHTML = html;
}

async function init() {
  const res = await fetch('/api/v1/state');
  state = await res.json();

  if (!selected) {
    loadDefaultSelection();
  }

  renderMeta();
  renderGroups();
  renderDetails();
  renderRegistry();
}

init().catch(err => {
  document.getElementById("groups").innerHTML = '<div class="empty">Failed to load state</div>';
  document.getElementById("details").innerHTML = '<pre>' + escapeHtml(err.stack || err.message) + '</pre>';
  document.getElementById("registry").innerHTML = '<div class="empty">Failed to load registry</div>';
});
</script>
</body>
</html>`
