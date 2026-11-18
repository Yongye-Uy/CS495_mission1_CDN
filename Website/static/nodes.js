// nodes.js - Storage Nodes Management

document.addEventListener("DOMContentLoaded", function () {
  loadNodes();

  const refreshBtn = document.getElementById("refreshNodesBtn");
  if (refreshBtn) {
    refreshBtn.addEventListener("click", loadNodes);
  }
});

async function loadNodes() {
  const tbody = document.getElementById("nodesTableBody");
  const countBadge = document.getElementById("nodeCountBadge");

  try {
    const res = await fetch("/api/nodes");
    if (!res.ok) {
      throw new Error(`HTTP ${res.status}`);
    }
    const data = await res.json();
    const nodes = data.nodes || [];

    countBadge.textContent = `${nodes.length} node(s) registered`;

    if (nodes.length === 0) {
      tbody.innerHTML = `
        <tr>
          <td colspan="8" class="text-center py-4 text-muted">
            No storage nodes currently registered. <a href="/nodes/add">Add the first node</a>.
          </td>
        </tr>
      `;
      return;
    }

    tbody.innerHTML = nodes.map(node => {
      let statusBadge = "bg-secondary";
      if (node.status === "ONLINE") statusBadge = "bg-dark";
      else if (node.status === "SYNCING") statusBadge = "bg-warning text-dark";
      else if (node.status === "OFFLINE" || node.status === "ERROR") statusBadge = "bg-danger";

      const coords = (node.latitude || node.longitude)
        ? `${node.latitude.toFixed(4)}, ${node.longitude.toFixed(4)}`
        : "—";

      const heartbeat = node.lastHeartbeat
        ? new Date(node.lastHeartbeat).toLocaleTimeString()
        : "Never";

      return `
        <tr>
          <td class="ps-3 fw-bold font-monospace small">${escapeHTML(node.nodeId || node.id)}</td>
          <td class="font-monospace small">${escapeHTML(node.ip)}:${node.port}</td>
          <td>${escapeHTML(node.region || "—")}</td>
          <td>${escapeHTML(node.country || "—")}</td>
          <td class="small text-muted">${coords}</td>
          <td><span class="badge ${statusBadge} px-2 py-1">${escapeHTML(node.status)}</span></td>
          <td class="small text-muted">${heartbeat}</td>
          <td class="text-end pe-3">
            <button class="btn btn-sm btn-outline-dark me-1" onclick="probeNodeHealth('${escapeHTML(node.id)}')">
              <i class="bi bi-activity me-1"></i>Health
            </button>
            <button class="btn btn-sm btn-danger" onclick="removeNode('${escapeHTML(node.id)}', '${escapeHTML(node.nodeId)}')">
              <i class="bi bi-trash3 me-1"></i>Remove
            </button>
          </td>
        </tr>
      `;
    }).join("");

  } catch (err) {
    console.error("Failed to load nodes:", err);
    tbody.innerHTML = `
      <tr>
        <td colspan="8" class="text-center py-4 text-danger">
          <i class="bi bi-exclamation-triangle me-1"></i>Failed to load storage nodes from Central API (${escapeHTML(err.message)})
        </td>
      </tr>
    `;
    countBadge.textContent = "Error loading";
  }
}

async function probeNodeHealth(id) {
  showAlert("Checking worker health...", "info");
  try {
    const res = await fetch(`/api/nodes/${encodeURIComponent(id)}/health`);
    const data = await res.json();
    if (res.ok && data.success) {
      showAlert(`Node ${data.node?.nodeId || id} is healthy and ONLINE.`, "success");
    } else {
      showAlert(`Node ${id} health check returned: ${data.status || "OFFLINE"} (${data.error || "no response"})`, "warning");
    }
    loadNodes();
  } catch (err) {
    showAlert(`Health check failed: ${err.message}`, "danger");
  }
}

async function removeNode(id, nodeId) {
  const displayName = nodeId || id;
  if (!confirm(`Are you sure you want to remove storage node "${displayName}"?\nThis node will no longer participate in CDN routing.`)) {
    return;
  }

  showAlert(`Removing node ${displayName}...`, "info");
  try {
    const res = await fetch(`/delete/${encodeURIComponent(id)}`, { method: "POST" });
    if (!res.ok) {
      // Fallback direct delete to API
      const direct = await fetch(`/api/nodes/${encodeURIComponent(id)}`, { method: "DELETE" });
      if (!direct.ok) {
        const errData = await direct.json().catch(() => ({}));
        throw new Error(errData.error || `HTTP ${direct.status}`);
      }
    }
    showAlert(`Storage node "${displayName}" removed successfully.`, "success");
    loadNodes();
  } catch (err) {
    showAlert(`Failed to remove node: ${err.message}`, "danger");
  }
}

function showAlert(msg, type) {
  const alert = document.getElementById("nodeActionAlert");
  if (!alert) return;
  alert.className = `alert alert-${type} small mb-3 py-2`;
  alert.innerHTML = msg;
  alert.classList.remove("d-none");
  setTimeout(() => {
    alert.classList.add("d-none");
  }, 6000);
}

function escapeHTML(str) {
  if (!str) return "";
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}
