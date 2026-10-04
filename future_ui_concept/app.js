const serviceData = [
  { id: "rtc-ap", product: "RTC", cluster: "rtc-zw", name: "RTC AP", status: "HEALTHY", expected: 2, discovered: 2, healthy: 2, unhealthy: 0, unknown: 0, level: "PORT", collected: "10:30:12", nodes: [
    node("rtc-ap-zw", "10.244.17.180", "agora_local_ap", "registry/agora_local_ap:3.1.3", "HEALTHY", "running", "PORT", "OK", "Native/Web 接入端口正常", 0, true),
    node("rtc-ap-iptv", "192.170.111.6", "agora_local_ap", "registry/agora_local_ap:3.1.3", "HEALTHY", "running", "PORT", "OK", "Native/Web 接入端口正常", 0, true),
  ]},
  { id: "rtc-balancer", product: "RTC", cluster: "rtc-zw", name: "RTC Balancer", status: "HEALTHY", expected: 2, discovered: 2, healthy: 2, unhealthy: 0, unknown: 0, level: "CONTAINER", collected: "10:30:11", nodes: [node("rtc-balancer-zw", "10.244.17.180", "agora_local_balancer", "registry/agora_local_balancer:1.4.1", "HEALTHY", "running", "CONTAINER", "OK", "容器运行正常", 0, true)] },
  { id: "rtc-vosync", product: "RTC", cluster: "rtc-zw", name: "RTC VoSync", status: "HEALTHY", expected: 2, discovered: 2, healthy: 2, unhealthy: 0, unknown: 0, level: "CONTAINER", collected: "10:30:11", nodes: [node("rtc-vosync-zw", "10.244.17.180", "agora_vosync", "registry/agora_vosync:1.8.6", "HEALTHY", "running", "CONTAINER", "OK", "容器运行正常", 0, true)] },
  { id: "rtc-capsync", product: "RTC", cluster: "rtc-zw", name: "RTC CapSync", status: "HEALTHY", expected: 2, discovered: 2, healthy: 2, unhealthy: 0, unknown: 0, level: "CONTAINER", collected: "10:30:11", nodes: [node("rtc-capsync-zw", "10.244.17.180", "agora_cap_sync", "registry/agora_cap_sync:1.1.6", "HEALTHY", "running", "CONTAINER", "OK", "容器运行正常", 0, true)] },
  { id: "rtc-udp-edge", product: "RTC", cluster: "rtc-zw", name: "RTC Native Edge UDP", status: "HEALTHY", expected: 2, discovered: 2, healthy: 2, unhealthy: 0, unknown: 0, level: "PORT", collected: "10:30:10", nodes: [node("rtc-udp-zw", "10.244.17.180", "agora_udp_media_edge_1", "registry/agora_udp_media_edge:4.0.10", "HEALTHY", "running", "PORT", "OK", "端口 4001 正常", 0, true)] },
  { id: "rtc-aut-edge", product: "RTC", cluster: "rtc-zw", name: "RTC Native Edge AUT", status: "HEALTHY", expected: 2, discovered: 2, healthy: 2, unhealthy: 0, unknown: 0, level: "PORT", collected: "10:30:10", nodes: [node("rtc-aut-zw", "10.244.17.180", "agora_aut_media_edge_1", "registry/agora_aut_media_edge:4.0.10", "HEALTHY", "running", "PORT", "OK", "端口 4701 正常", 0, true)] },
  { id: "rtc-web-edge", product: "RTC", cluster: "rtc-zw", name: "RTC Web Edge", status: "HEALTHY", expected: 2, discovered: 2, healthy: 2, unhealthy: 0, unknown: 0, level: "PORT", collected: "10:30:10", nodes: [node("rtc-web-zw", "10.244.17.180", "agora_web_media_edge_1", "registry/agora_web_media_edge:4.0.9", "HEALTHY", "running", "PORT", "OK", "TLS 端口 4501 正常", 0, true)] },
  { id: "rtc-event", product: "RTC", cluster: "rtc-zw", name: "RTC Event Collector", status: "HEALTHY", expected: 2, discovered: 2, healthy: 2, unhealthy: 0, unknown: 0, level: "CONTAINER", collected: "10:30:10", nodes: [node("rtc-event-zw", "10.244.17.180", "agora_event_collector", "registry/agora_event_collector:1.10.3", "HEALTHY", "running", "CONTAINER", "OK", "容器运行正常", 0, true)] },
  { id: "rtc-infra", product: "RTC", cluster: "rtc-zw", name: "RTC Infra Helper", status: "HEALTHY", expected: 2, discovered: 2, healthy: 2, unhealthy: 0, unknown: 0, level: "CONTAINER", collected: "10:30:09", nodes: [node("rtc-infra-zw", "10.244.17.180", "agora_infra_helper", "registry/agora_infra_helper:1.1.16", "HEALTHY", "running", "CONTAINER", "OK", "容器运行正常", 0, false)] },
  { id: "rtm-config", product: "RTM", cluster: "rtm-zw", name: "RTM Config", status: "HEALTHY", expected: 6, discovered: 6, healthy: 6, unhealthy: 0, unknown: 0, level: "PORT", collected: "10:30:12", nodes: [node("rtm-config-zw", "10.244.17.218", "config_server", "rtm/config_server:2.2.4", "HEALTHY", "running", "PORT", "OK", "配置服务端口正常", 0, true)] },
  { id: "rtm-forwarder", product: "RTM", cluster: "rtm-zw", name: "RTM Core Forwarder", status: "DEGRADED", expected: 4, discovered: 3, healthy: 3, unhealthy: 1, unknown: 0, level: "CONTAINER", collected: "10:30:12", nodes: [
    node("rtm-forwarder0", "10.244.17.218", "forwarder0", "rtm/forwarder:2.2.4", "HEALTHY", "running", "CONTAINER", "OK", "容器运行正常", 0, true),
    node("rtm-forwarder1", "10.244.17.218", "forwarder1", "rtm/forwarder:2.2.4", "HEALTHY", "running", "CONTAINER", "OK", "容器运行正常", 0, true),
    node("rtm-forwarder2", "10.244.17.218", "forwarder2", "rtm/forwarder:2.2.4", "HEALTHY", "running", "CONTAINER", "OK", "容器运行正常", 0, true),
    node("rtm-forwarder3", "10.244.17.218", "forwarder3", "rtm/forwarder:2.2.4", "UNHEALTHY", "missing", "CONTAINER", "CONTAINER_MISSING", "应有容器未出现在最新快照中", 0, false),
  ]},
  { id: "rtm-distributor", product: "RTM", cluster: "rtm-zw", name: "RTM Core Distributor", status: "HEALTHY", expected: 4, discovered: 4, healthy: 4, unhealthy: 0, unknown: 0, level: "CONTAINER", collected: "10:30:12", nodes: [node("rtm-distributor0", "10.244.17.218", "distributor0", "rtm/distributor:2.2.4", "HEALTHY", "running", "CONTAINER", "OK", "容器运行正常", 0, true)] },
  { id: "rtm-msgcache", product: "RTM", cluster: "rtm-zw", name: "RTM Core Message Cache", status: "HEALTHY", expected: 4, discovered: 4, healthy: 4, unhealthy: 0, unknown: 0, level: "PORT", collected: "10:30:11", nodes: [node("rtm-msgcache0", "10.244.17.218", "msgcache0", "rtm/msgcache:2.2.4", "HEALTHY", "running", "PORT", "OK", "端口 18511 正常", 0, true)] },
  { id: "rtm-user-state", product: "RTM", cluster: "rtm-zw", name: "RTM Core User State", status: "HEALTHY", expected: 4, discovered: 4, healthy: 4, unhealthy: 0, unknown: 0, level: "CONTAINER", collected: "10:30:11", nodes: [node("rtm-userwatch0", "10.244.17.218", "userwatch0", "rtm/userwatch:0.0.1", "HEALTHY", "running", "CONTAINER", "OK", "容器运行正常", 1, true)] },
  { id: "rtm-group-state", product: "RTM", cluster: "rtm-zw", name: "RTM Core Group State", status: "HEALTHY", expected: 4, discovered: 4, healthy: 4, unhealthy: 0, unknown: 0, level: "CONTAINER", collected: "10:30:11", nodes: [node("rtm-groupwatch0", "10.244.17.218", "groupwatch0", "rtm/groupwatch:0.0.1", "HEALTHY", "running", "CONTAINER", "OK", "容器运行正常", 0, true)] },
  { id: "rtm-registrar", product: "RTM", cluster: "rtm-zw", name: "RTM Edge Registrar", status: "UNKNOWN", expected: 2, discovered: 2, healthy: 1, unhealthy: 0, unknown: 1, level: "AGENT", collected: "10:29:23", nodes: [
    node("rtm-registrar-zw", "10.244.17.218", "registrar0", "rtm/registrar:2.2.4", "HEALTHY", "running", "CONTAINER", "OK", "容器运行正常", 0, true),
    node("rtm-registrar-iptv", "192.170.111.5", "registrar0", "rtm/registrar:2.2.4", "UNKNOWN", "unknown", "AGENT", "STATUS_STALE", "Agent 状态已超过 45 秒", 0, false),
  ]},
  { id: "rtm-webgateway", product: "RTM", cluster: "rtm-zw", name: "RTM Edge WebGateway", status: "HEALTHY", expected: 8, discovered: 8, healthy: 8, unhealthy: 0, unknown: 0, level: "PORT", collected: "10:30:10", nodes: [node("rtm-webgateway0", "10.244.17.218", "webgateway0", "rtm/webgateway:2.2.4", "HEALTHY", "running", "PORT", "OK", "端口 9550 正常", 0, true)] },
  { id: "rtm-rest", product: "RTM", cluster: "rtm-zw", name: "RTM REST", status: "HEALTHY", expected: 2, discovered: 2, healthy: 2, unhealthy: 0, unknown: 0, level: "HTTP", collected: "10:30:10", nodes: [node("rtm-restful", "10.244.17.218", "rtm_restful", "rtm/restful:2.2.4", "HEALTHY", "running", "HTTP", "OK", "/health 返回 200", 0, true)] },
  { id: "rtm-sync", product: "RTM", cluster: "rtm-sync", name: "RTM Sync", status: "HEALTHY", expected: 3, discovered: 3, healthy: 3, unhealthy: 0, unknown: 0, level: "PORT", collected: "10:30:09", nodes: [node("rtm-sync-promise", "172.31.11.128", "crsync-sync-promise-bak-", "online/promise:1.7", "HEALTHY", "running", "PORT", "OK", "端口 8389 正常", 0, true)] },
];

const expectedContainers = {
  "rtc-balancer": ["agora_local_balancer", "agora_local_balancer"],
  "rtc-vosync": ["agora_vosync", "agora_vosync"],
  "rtc-capsync": ["agora_cap_sync", "agora_cap_sync"],
  "rtc-udp-edge": ["agora_udp_media_edge_1", "agora_udp_media_edge_1"],
  "rtc-aut-edge": ["agora_aut_media_edge_1", "agora_aut_media_edge_1"],
  "rtc-web-edge": ["agora_web_media_edge_1", "agora_web_media_edge_1"],
  "rtc-event": ["agora_event_collector", "agora_event_collector"],
  "rtc-infra": ["agora_infra_helper", "agora_infra_helper"],
  "rtm-config": ["configurator", "config_server", "config_proxy", "config_gateway", "local_idc_config", "signalingconfig"],
  "rtm-distributor": ["distributor0", "distributor1", "distributor2", "distributor3"],
  "rtm-msgcache": ["msgcache0", "msgcache1", "msgcache2", "msgcache3"],
  "rtm-user-state": ["userwatch0", "userwatch1", "usernoter0", "usernoter1"],
  "rtm-group-state": ["groupwatch0", "groupwatch1", "groupnoter0", "groupnoter1"],
  "rtm-webgateway": ["webgateway0", "webgateway1", "webgateway2", "webgateway3", "webgateway4", "webgateway5", "webgateway6", "webgateway7"],
  "rtm-rest": ["rtm_restful", "restful_nginx"],
  "rtm-sync": ["sync-coordinator-server", "sync-coordinator-sidecar", "crsync-sync-promise-bak-"],
};

serviceData.forEach((service) => {
  const containerNames = expectedContainers[service.id] || [];
  const seed = service.nodes[0];
  while (service.nodes.length < service.expected) {
    const index = service.nodes.length;
    const isRTC = service.product === "RTC";
    const host = isRTC ? ["10.244.17.180", "192.170.111.6"][index % 2] : seed.host;
    const container = containerNames[index] || `${seed.container}_${index}`;
    service.nodes.push(node(`${service.id}-${index}`, host, container, seed.image, "HEALTHY", "running", service.level, "OK", seed.reason, 0, seed.restartEnabled));
  }
});

function node(id, host, container, image, status, runtime, level, reasonCode, reason, restarts, restartEnabled) {
  return { id, host, hostName: hostName(host), container, image, status, runtime, level, reasonCode, reason, restarts, restartEnabled, collected: status === "UNKNOWN" ? "10:29:23" : "10:30:12", uptime: status === "UNHEALTHY" ? "-" : "18天 7小时" };
}

function hostName(host) {
  const names = { "10.244.17.180": "rtc-zw-edge-01", "192.170.111.6": "rtc-iptv-edge-01", "10.244.17.218": "rtm-zw-01", "192.170.111.5": "rtm-iptv-01", "172.31.11.128": "rtm-sync-01" };
  return names[host] || host;
}

const incidents = [
  { id: "INC-20260914-002", severity: "P0", state: "OPEN", product: "RTM", cluster: "rtm-zw", service: "RTM Core Forwarder", node: "rtm-zw-01 / forwarder3", reason: "CONTAINER_MISSING", message: "应有容器未出现在最新快照中", started: "今天 10:12:04", duration: "18 分钟", nodeId: "rtm-forwarder3" },
  { id: "INC-20260914-001", severity: "P1", state: "OPEN", product: "RTM", cluster: "rtm-iptv", service: "RTM Edge Registrar", node: "rtm-iptv-01 / registrar0", reason: "STATUS_STALE", message: "Agent 状态已超过 45 秒", started: "今天 10:29:23", duration: "1 分钟", nodeId: "rtm-registrar-iptv" },
  { id: "INC-20260913-006", severity: "P1", state: "RESOLVED", product: "RTC", cluster: "rtc-zw", service: "RTC Web Edge", node: "rtc-zw-edge-01 / agora_web_media_edge_1", reason: "TCP_CHECK_FAILED", message: "TLS 端口连续 3 次连接失败", started: "昨天 22:14:36", duration: "6 分钟", nodeId: "rtc-web-zw" },
  { id: "INC-20260912-003", severity: "P2", state: "RESOLVED", product: "RTM", cluster: "rtm-zw", service: "RTM Core User State", node: "rtm-zw-01 / userwatch0", reason: "RESTART_COUNT_CHANGED", message: "容器重启次数从 0 增加到 1", started: "9月12日 16:42:11", duration: "2 分钟", nodeId: "rtm-userwatch0" },
  { id: "INC-20260911-001", severity: "P1", state: "RESOLVED", product: "RTC", cluster: "rtc-iptv", service: "RTC AP", node: "rtc-iptv-edge-01 / agora_local_ap", reason: "CONTAINER_EXITED", message: "容器异常退出，exitCode=137", started: "9月11日 08:21:04", duration: "4 分钟", nodeId: "rtc-ap-iptv" },
];

let operations = [
  { id: "OP-20260914-003", status: "RUNNING", target: "rtc-zw-edge-01 / agora_local_ap", operator: "韩小青", reason: "计划维护重启", started: "今天 10:26:31", duration: "执行中", after: "复查中" },
  { id: "OP-20260914-002", status: "SUCCESS", target: "rtc-zw-edge-01 / agora_web_media_edge_1", operator: "商文海", reason: "TLS 端口异常，人工重启", started: "今天 09:45:18", duration: "8.2 秒", after: "HEALTHY" },
  { id: "OP-20260914-001", status: "SUCCESS", target: "rtm-zw-01 / userwatch0", operator: "孙亮", reason: "容器重启次数异常增加", started: "今天 08:32:44", duration: "6.7 秒", after: "HEALTHY" },
];

let activeProduct = "ALL";
let activeIncidentState = "OPEN";

const titles = { overview: "运行总览", services: "服务监控", incidents: "异常事件", operations: "运维操作", baseline: "服务基线" };

function statusLabel(status) {
  return { HEALTHY: "正常", DEGRADED: "部分异常", UNHEALTHY: "异常", UNKNOWN: "未知", OPEN: "未恢复", RESOLVED: "已恢复", SUCCESS: "成功", FAILED: "失败", RUNNING: "执行中" }[status] || status;
}

function statusClass(status) { return status.toLowerCase(); }
function statusPill(status) { return `<span class="status-pill ${statusClass(status)}">${statusLabel(status)}</span>`; }
function inlineStatus(status) { return `<span class="inline-status ${statusClass(status)}"><i></i>${statusLabel(status)}</span>`; }
function escapeHtml(value) { return String(value ?? "").replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;"); }

function renderOverview() {
  const totals = serviceData.reduce((result, item) => {
    result.expected += item.expected;
    result.discovered += item.discovered;
    result.healthy += item.healthy;
    result.unhealthy += item.unhealthy;
    result.unknown += item.unknown;
    return result;
  }, { expected: 0, discovered: 0, healthy: 0, unhealthy: 0, unknown: 0 });
  const rtc = serviceData.filter((item) => item.product === "RTC");
  const rtm = serviceData.filter((item) => item.product === "RTM");
  const productTotals = (items) => items.reduce((result, item) => ({ expected: result.expected + item.expected, healthy: result.healthy + item.healthy }), { expected: 0, healthy: 0 });
  const rtcTotals = productTotals(rtc);
  const rtmTotals = productTotals(rtm);
  document.getElementById("summary-services").textContent = serviceData.length;
  document.getElementById("summary-products").textContent = `RTC ${rtc.length} · RTM ${rtm.length}`;
  document.getElementById("summary-expected").textContent = totals.expected;
  document.getElementById("summary-discovered").textContent = `已发现 ${totals.discovered}`;
  document.getElementById("summary-healthy").textContent = totals.healthy;
  document.getElementById("summary-health-rate").textContent = `${((totals.healthy / totals.expected) * 100).toFixed(1)}%`;
  document.getElementById("summary-problem").textContent = `${totals.unhealthy} / ${totals.unknown}`;
  document.getElementById("rtc-summary-detail").textContent = `${rtc.length} 个服务 · ${rtcTotals.expected} 个节点`;
  document.getElementById("rtc-summary-count").textContent = `${rtcTotals.healthy} / ${rtcTotals.expected}`;
  document.getElementById("rtm-summary-detail").textContent = `${rtm.length} 个服务 · ${rtmTotals.expected} 个节点`;
  document.getElementById("rtm-summary-count").textContent = `${rtmTotals.healthy} / ${rtmTotals.expected}`;
  const critical = serviceData.filter((item) => ["rtc-ap", "rtc-web-edge", "rtm-forwarder", "rtm-registrar", "rtm-sync"].includes(item.id));
  document.getElementById("overview-service-table").innerHTML = `<table class="compact-table"><thead><tr><th>产品</th><th>服务</th><th>状态</th><th>节点</th><th>检查</th></tr></thead><tbody>${critical.map((item) => `<tr data-service-id="${item.id}"><td><span class="mini-code ${item.product.toLowerCase()}">${item.product}</span></td><td><strong>${item.name}</strong><div class="stat-label">${item.cluster}</div></td><td>${inlineStatus(item.status)}</td><td><span class="node-ratio">${item.healthy}/${item.expected}</span></td><td>${item.level}</td></tr>`).join("")}</tbody></table>`;
  document.getElementById("overview-incidents").innerHTML = incidents.filter((item) => item.state === "OPEN").map((item) => `<div class="incident-item" data-node-id="${item.nodeId}"><span class="incident-level">${item.severity}</span><div><strong>${item.service}</strong><p>${item.node}<br>${item.message}</p></div><time>${item.duration}</time></div>`).join("");
  bindDetailOpeners();
}

function renderServices() {
  const search = document.getElementById("service-search").value.trim().toLowerCase();
  const status = document.getElementById("status-filter").value;
  const filtered = serviceData.filter((item) => {
    const matchesProduct = activeProduct === "ALL" || item.product === activeProduct;
    const matchesStatus = status === "ALL" || item.status === status;
    const haystack = `${item.product} ${item.cluster} ${item.name} ${item.nodes.map((node) => `${node.host} ${node.hostName} ${node.container}`).join(" ")}`.toLowerCase();
    return matchesProduct && matchesStatus && (!search || haystack.includes(search));
  });
  document.getElementById("service-result-count").textContent = `${filtered.length} 个逻辑服务`;
  document.getElementById("service-table-body").innerHTML = filtered.map((item) => `<tr data-service-id="${item.id}"><td><div class="product-cell"><span class="mini-code ${item.product.toLowerCase()}">${item.product}</span><span><strong>${item.product}</strong><small>${item.cluster}</small></span></div></td><td><strong>${item.name}</strong><div class="stat-label">${item.id}</div></td><td>${statusPill(item.status)}</td><td>${item.expected}</td><td class="${item.discovered < item.expected ? "count-bad" : ""}">${item.discovered}</td><td>${item.healthy}</td><td class="${item.unhealthy || item.unknown ? "count-bad" : ""}">${item.unhealthy + item.unknown}</td><td>${item.level}</td><td>${item.collected}</td></tr>`).join("");
  bindDetailOpeners();
}

function renderIncidents() {
  const filtered = incidents.filter((item) => activeIncidentState === "ALL" || item.state === activeIncidentState);
  document.getElementById("incident-table-body").innerHTML = filtered.map((item) => `<tr data-node-id="${item.nodeId}"><td><span class="incident-level">${item.severity}</span></td><td>${statusPill(item.state)}</td><td><strong>${item.product}</strong><div class="stat-label">${item.cluster}</div></td><td><strong>${item.service}</strong><div class="stat-label">${item.node}</div></td><td><code>${item.reason}</code><div class="stat-label">${item.message}</div></td><td>${item.started}</td><td>${item.duration}</td></tr>`).join("");
  bindDetailOpeners();
}

function renderOperations() {
  document.getElementById("operation-total").textContent = operations.length;
  document.getElementById("operation-success").textContent = operations.filter((item) => item.status === "SUCCESS").length;
  document.getElementById("operation-failed").textContent = operations.filter((item) => item.status === "FAILED").length;
  document.getElementById("operation-running").textContent = operations.filter((item) => item.status === "RUNNING").length;
  document.getElementById("operation-table-body").innerHTML = operations.map((item) => `<tr><td><code>${item.id}</code></td><td>${statusPill(item.status)}</td><td>${item.target}</td><td><strong>${item.operator}</strong><div class="stat-label">${item.reason}</div></td><td>${item.started}</td><td>${item.duration}</td><td>${item.after === "HEALTHY" ? inlineStatus("HEALTHY") : `<span class="warning-text">${item.after}</span>`}</td></tr>`).join("");
}

function renderBaseline() {
  const rows = serviceData.flatMap((service) => service.nodes.map((item) => ({ ...item, service })));
  document.getElementById("baseline-table-body").innerHTML = rows.map(({ service, ...item }) => `<tr data-node-id="${item.id}"><td><code>${item.id}</code></td><td><div class="product-cell"><span class="mini-code ${service.product.toLowerCase()}">${service.product}</span><span><strong>${service.product}</strong><small>${service.cluster}</small></span></div></td><td>${service.name}</td><td>${item.hostName}<div class="stat-label">${item.host}</div></td><td><code>${item.container}</code></td><td>${item.level}</td><td>${item.restartEnabled ? `<span class="toggle-readonly"></span>` : "否"}</td></tr>`).join("");
  bindDetailOpeners();
}

function findNode(nodeId) {
  for (const service of serviceData) {
    const found = service.nodes.find((item) => item.id === nodeId);
    if (found) return { service, node: found };
  }
  return null;
}

function openService(serviceId) {
  const service = serviceData.find((item) => item.id === serviceId);
  if (!service) return;
  openDrawer(service, service.nodes[0], true);
}

function openNode(nodeId) {
  const found = findNode(nodeId);
  if (found) openDrawer(found.service, found.node, false);
}

function openDrawer(service, selectedNode, showAll) {
  document.getElementById("drawer-kicker").textContent = `${service.product} · ${service.cluster}`;
  document.getElementById("drawer-title").textContent = showAll ? service.name : selectedNode.container;
  const nodeRows = (showAll ? service.nodes : [selectedNode]).map((item) => `<div class="evidence-item"><div><strong>${item.hostName} / ${item.container}</strong><small>${item.host} · ${item.reason}</small></div>${statusPill(item.status)}</div>`).join("");
  const node = selectedNode;
  document.getElementById("drawer-content").innerHTML = `<div class="drawer-body"><div class="drawer-status"><div><strong>${service.name}</strong><p>${node.reasonCode} · ${node.reason}</p></div>${statusPill(node.status)}</div><section class="detail-section"><h3>节点信息</h3><div class="detail-grid"><div><span>宿主机</span><strong>${node.hostName}<br>${node.host}</strong></div><div><span>容器</span><strong>${node.container}</strong></div><div><span>镜像版本</span><strong>${node.image}</strong></div><div><span>运行时长</span><strong>${node.uptime}</strong></div><div><span>检查层级</span><strong>${node.level}</strong></div><div><span>重启次数</span><strong>${node.restarts}</strong></div><div><span>应有 / 发现</span><strong>${service.expected} / ${service.discovered}</strong></div><div><span>最近采集</span><strong>今天 ${node.collected}</strong></div></div></section><section class="detail-section"><h3>${showAll ? "服务节点" : "状态证据"}</h3><div class="evidence-list">${showAll ? nodeRows : evidenceFor(node)}</div></section><div class="drawer-actions"><button type="button" class="secondary-btn" id="drawer-incident-btn">查看相关事件</button><button type="button" class="danger-btn" id="drawer-restart-btn" ${node.restartEnabled && node.runtime !== "missing" && node.status !== "UNKNOWN" ? "" : "disabled"}>重启此容器</button></div></div>`;
  document.getElementById("detail-drawer").classList.add("open");
  document.getElementById("drawer-backdrop").classList.add("open");
  document.getElementById("detail-drawer").setAttribute("aria-hidden", "false");
  document.getElementById("drawer-restart-btn").addEventListener("click", () => openRestartModal(service, node));
  document.getElementById("drawer-incident-btn").addEventListener("click", () => { closeDrawer(); setView("incidents"); });
}

function evidenceFor(node) {
  const containerResult = node.runtime === "running" ? ["HEALTHY", "Docker 容器运行中"] : node.runtime === "missing" ? ["UNHEALTHY", "应有容器未发现"] : ["UNKNOWN", "运行态无法确认"];
  const second = node.level === "PORT" || node.level === "HTTP" ? [node.status, node.reason] : ["UNKNOWN", "该节点未配置服务级探测"];
  return `<div class="evidence-item"><div><strong>L1 容器状态</strong><small>${containerResult[1]}</small></div>${statusPill(containerResult[0])}</div><div class="evidence-item"><div><strong>L2 服务探测</strong><small>${second[1]}</small></div>${statusPill(second[0])}</div><div class="evidence-item"><div><strong>L4 业务黑盒</strong><small>计划在 V1.1 接入</small></div>${statusPill("UNKNOWN")}</div>`;
}

function closeDrawer() {
  document.getElementById("detail-drawer").classList.remove("open");
  document.getElementById("drawer-backdrop").classList.remove("open");
  document.getElementById("detail-drawer").setAttribute("aria-hidden", "true");
}

function openRestartModal(service, node) {
  closeDrawer();
  document.getElementById("restart-node-id").value = node.id;
  document.getElementById("restart-target").textContent = `${node.hostName} / ${node.container}`;
  document.getElementById("restart-impact").textContent = `${service.name} · ${service.cluster}；重启期间该节点会短暂不可用`;
  document.getElementById("restart-reason").value = "";
  document.getElementById("restart-confirm").checked = false;
  document.getElementById("restart-modal").classList.add("open");
  document.getElementById("restart-modal").setAttribute("aria-hidden", "false");
  document.getElementById("restart-reason").focus();
}

function closeRestartModal() {
  document.getElementById("restart-modal").classList.remove("open");
  document.getElementById("restart-modal").setAttribute("aria-hidden", "true");
}

function submitRestart(event) {
  event.preventDefault();
  const nodeId = document.getElementById("restart-node-id").value;
  const found = findNode(nodeId);
  if (!found) return;
  const operator = document.getElementById("restart-operator").value.trim();
  const reason = document.getElementById("restart-reason").value.trim();
  const operation = { id: `OP-20260914-${String(operations.length + 1).padStart(3, "0")}`, status: "RUNNING", target: `${found.node.hostName} / ${found.node.container}`, operator, reason, started: `今天 ${new Date().toLocaleTimeString("zh-CN", { hour12: false })}`, duration: "执行中", after: "复查中" };
  operations.unshift(operation);
  closeRestartModal();
  renderOperations();
  setView("operations");
  showToast(`已下发重启任务：${found.node.container}`);
  window.setTimeout(() => {
    operation.status = "SUCCESS";
    operation.duration = "7.4 秒";
    operation.after = "HEALTHY";
    found.node.status = "HEALTHY";
    found.node.runtime = "running";
    found.node.reasonCode = "OK";
    found.node.reason = "重启后容器运行正常";
    found.node.restarts += 1;
    renderOperations();
    showToast(`${found.node.container} 已恢复正常`);
  }, 1800);
}

function setView(view) {
  document.querySelectorAll(".view").forEach((item) => item.classList.toggle("active", item.id === `view-${view}`));
  document.querySelectorAll(".nav-item").forEach((item) => item.classList.toggle("active", item.dataset.view === view));
  document.getElementById("page-title").textContent = titles[view];
  document.getElementById("breadcrumb-current").textContent = titles[view];
  if (view === "services") renderServices();
  if (view === "incidents") renderIncidents();
  if (view === "operations") renderOperations();
  if (view === "baseline") renderBaseline();
  window.scrollTo({ top: 0, behavior: "smooth" });
}

function bindDetailOpeners() {
  document.querySelectorAll("[data-service-id]").forEach((row) => row.addEventListener("click", () => openService(row.dataset.serviceId)));
  document.querySelectorAll("[data-node-id]").forEach((row) => row.addEventListener("click", () => openNode(row.dataset.nodeId)));
}

function showToast(message) {
  const toast = document.getElementById("toast");
  toast.textContent = message;
  toast.classList.add("show");
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(() => toast.classList.remove("show"), 2400);
}

document.querySelectorAll(".nav-item").forEach((button) => button.addEventListener("click", () => setView(button.dataset.view)));
document.querySelectorAll("[data-jump]").forEach((button) => button.addEventListener("click", () => setView(button.dataset.jump)));
document.querySelectorAll("[data-product-filter]").forEach((button) => button.addEventListener("click", () => {
  activeProduct = button.dataset.productFilter;
  document.querySelectorAll("#product-filter button").forEach((item) => item.classList.toggle("active", item.dataset.product === activeProduct));
  setView("services");
}));
document.querySelectorAll("#product-filter button").forEach((button) => button.addEventListener("click", () => {
  activeProduct = button.dataset.product;
  document.querySelectorAll("#product-filter button").forEach((item) => item.classList.toggle("active", item === button));
  renderServices();
}));
document.querySelectorAll("[data-incident-state]").forEach((button) => button.addEventListener("click", () => {
  activeIncidentState = button.dataset.incidentState;
  document.querySelectorAll("[data-incident-state]").forEach((item) => item.classList.toggle("active", item === button));
  renderIncidents();
}));
document.getElementById("service-search").addEventListener("input", renderServices);
document.getElementById("status-filter").addEventListener("change", renderServices);
document.getElementById("drawer-close").addEventListener("click", closeDrawer);
document.getElementById("drawer-backdrop").addEventListener("click", closeDrawer);
document.getElementById("restart-close").addEventListener("click", closeRestartModal);
document.getElementById("restart-cancel").addEventListener("click", closeRestartModal);
document.getElementById("restart-modal").addEventListener("click", (event) => { if (event.target.id === "restart-modal") closeRestartModal(); });
document.getElementById("restart-form").addEventListener("submit", submitRestart);
document.getElementById("refresh-btn").addEventListener("click", () => {
  const now = new Date().toLocaleTimeString("zh-CN", { hour12: false });
  document.getElementById("last-sync").textContent = now;
  showToast("状态已刷新");
});
document.getElementById("validate-baseline").addEventListener("click", () => showToast("基线校验通过：42 个应有节点，1 个节点缺失"));
document.getElementById("add-node").addEventListener("click", () => showToast("原型：进入新增节点流程"));
document.getElementById("export-incidents").addEventListener("click", () => showToast("原型：异常事件导出任务已创建"));
document.getElementById("site-menu").addEventListener("click", () => showToast("当前仅配置客户生产环境"));
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape") { closeDrawer(); closeRestartModal(); }
});

renderOverview();
renderServices();
renderIncidents();
renderOperations();
renderBaseline();
