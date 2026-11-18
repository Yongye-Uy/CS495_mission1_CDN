// CDN Client Application — Simple Clean Black & White
document.addEventListener("DOMContentLoaded", () => {
  const API_ENDPOINT = "/api";

  // Real client location state
  window.realClientCountry = "KH";
  window.realClientLocationName = "Detecting location...";

  // DOM Elements
  const clientLocationText = document.getElementById("clientLocationText");
  const refreshBtn = document.getElementById("refreshBtn");

  const dropzone = document.getElementById("dropzone");
  const browseBtn = document.getElementById("browseBtn");
  const fileInput = document.getElementById("fileInput");
  const selectedFileInfo = document.getElementById("selectedFileInfo");
  const selectedFileName = document.getElementById("selectedFileName");
  const selectedFileSize = document.getElementById("selectedFileSize");
  const uploadBtn = document.getElementById("uploadBtn");
  const cancelSelectBtn = document.getElementById("cancelSelectBtn");
  const uploadFeedback = document.getElementById("uploadFeedback");

  const searchInput = document.getElementById("searchInput");
  const fileCountText = document.getElementById("fileCountText");
  const filesTableBody = document.getElementById("filesTableBody");

  // Storage Nodes DOM Elements (Home page)
  const nodeCountBadge = document.getElementById("nodeCountBadge");
  const nodesTableBody = document.getElementById("nodesTableBody");
  const nodeActionAlert = document.getElementById("nodeActionAlert");
  const toggleAddNodeBtn = document.getElementById("toggleAddNodeBtn");
  const closeAddNodeBtn = document.getElementById("closeAddNodeBtn");
  const cancelHomeAddNodeBtn = document.getElementById("cancelHomeAddNodeBtn");
  const addNodeCollapse = document.getElementById("addNodeCollapse");
  const homeAddNodeForm = document.getElementById("homeAddNodeForm");
  const homeSubmitNodeBtn = document.getElementById("homeSubmitNodeBtn");
  const homeProgressBox = document.getElementById("homeProgressBox");
  const homeProgressAlert = document.getElementById("homeProgressAlert");
  const homeProgressSpinner = document.getElementById("homeProgressSpinner");

  // Bootstrap 5 Modals
  const locationModalElem = document.getElementById("locationModal");
  const locationModalBody = document.getElementById("locationModalBody");
  const locationModal = window.bootstrap ? bootstrap.Modal.getOrCreateInstance(locationModalElem) : null;

  const previewModalElem = document.getElementById("previewModal");
  const previewTitle = document.getElementById("previewTitle");
  const previewBody = document.getElementById("previewBody");
  const previewDownloadLink = document.getElementById("previewDownloadLink");
  const previewModal = window.bootstrap ? bootstrap.Modal.getOrCreateInstance(previewModalElem) : null;

  let selectedFile = null;
  let allFiles = [];

  function formatBytes(bytes) {
    if (bytes === 0) return "0 Bytes";
    const k = 1024;
    const sizes = ["Bytes", "KB", "MB", "GB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
  }

  function formatDate(isoStr) {
    if (!isoStr) return "-";
    const d = new Date(isoStr);
    return d.toLocaleString();
  }

  function escapeHtml(str) {
    if (!str) return "";
    return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  }

  // Format replica location: map "local" or empty to "SINGAPORE", otherwise uppercase
  window.formatReplicaLocation = function (loc) {
    if (!loc || loc.toLowerCase() === "local") {
      return "SINGAPORE";
    }
    return loc.toUpperCase();
  };

  // 1. Detect Real Client Location (Read-only, automatic)
  async function detectRealClientLocation() {
    if (clientLocationText) clientLocationText.textContent = "Detecting...";

    // Strategy A: Direct browser lookup via free public geoip
    try {
      const res = await fetch("https://ipapi.co/json/", { signal: AbortSignal.timeout(2500) });
      if (res.ok) {
        const data = await res.json();
        const country = data.country_name || data.country;
        if (country) {
          const locStr = data.city ? `${data.city}, ${country}` : country;
          window.realClientCountry = data.country_code || data.country || "KH";
          window.realClientLocationName = locStr;
          if (clientLocationText) clientLocationText.textContent = locStr;
          return;
        }
      }
    } catch (e) {}

    // Strategy B: Lightweight fallback API
    try {
      const res2 = await fetch("https://api.country.is", { signal: AbortSignal.timeout(2000) });
      if (res2.ok) {
        const data2 = await res2.json();
        if (data2.country) {
          window.realClientCountry = data2.country;
          window.realClientLocationName = data2.country;
          if (clientLocationText) clientLocationText.textContent = data2.country;
          return;
        }
      }
    } catch (e) {}

    // Strategy C: Central API's detected location
    try {
      const res3 = await fetch(`${API_ENDPOINT}/client-location`, { signal: AbortSignal.timeout(2000) });
      if (res3.ok) {
        const data3 = await res3.json();
        window.realClientCountry = data3.countryCode || data3.country || "KH";
        window.realClientLocationName = data3.country || "Cambodia";
        if (clientLocationText) clientLocationText.textContent = data3.country || "Cambodia";
        return;
      }
    } catch (e) {}

    // Ultimate default
    window.realClientCountry = "KH";
    window.realClientLocationName = "Cambodia";
    if (clientLocationText) clientLocationText.textContent = "Cambodia";
  }

  // 2. Fetch and render File List
  async function fetchFiles() {
    try {
      const res = await fetch(`${API_ENDPOINT}/files`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      allFiles = await res.json();
      renderFiles(allFiles);
    } catch (err) {
      if (filesTableBody) {
        filesTableBody.innerHTML = `
          <tr>
            <td colspan="7" class="text-center py-4 text-danger">
              <i class="bi bi-exclamation-circle me-1"></i>Failed to load files from Central API: ${escapeHtml(err.message)}
            </td>
          </tr>`;
      }
      if (fileCountText) fileCountText.textContent = "Error fetching files";
    }
  }

  function renderFiles(files) {
    if (fileCountText) {
      fileCountText.textContent = `(${files.length} indexed)`;
    }

    if (!files || files.length === 0) {
      filesTableBody.innerHTML = `
        <tr>
          <td colspan="7" class="text-center py-4 text-muted">
            No files uploaded yet. Select or drop a file above to store and replicate across storage nodes.
          </td>
        </tr>`;
      return;
    }

    filesTableBody.innerHTML = files.map(f => {
      const replicasBadges = (f.replicas || []).map(r =>
        `<span class="badge bg-black text-white me-1 border" title="${r.url}"><i class="bi bi-hdd me-1"></i>${formatReplicaLocation(r.location)}</span>`
      ).join(" ");

      return `
        <tr data-id="${f.id}" data-name="${escapeHtml(f.originalName)}">
          <td class="ps-3"><strong>${escapeHtml(f.originalName)}</strong></td>
          <td><code class="file-code-id">${f.id}</code></td>
          <td>${formatBytes(f.size)}</td>
          <td><span class="text-muted small">${escapeHtml(f.contentType || "binary")}</span></td>
          <td><span class="text-muted small">${formatDate(f.createdAt)}</span></td>
          <td>${replicasBadges || '<span class="badge border border-dark text-dark">No replicas</span>'}</td>
          <td class="text-end pe-3">
            <div class="btn-group btn-group-sm" role="group">
              <button class="btn btn-outline-dark inspect-btn" data-id="${f.id}" title="Route Diagnostics">
                <i class="bi bi-geo-alt me-1"></i>Route
              </button>
              <button class="btn btn-outline-dark preview-btn" data-id="${f.id}" data-name="${escapeHtml(f.originalName)}" data-type="${escapeHtml(f.contentType || "")}" title="Preview">
                <i class="bi bi-eye me-1"></i>View
              </button>
              <button class="btn btn-dark download-btn" data-id="${f.id}" title="Download">
                <i class="bi bi-download me-1"></i>Download
              </button>
              <!-- Delete is RED -->
              <button class="btn btn-danger delete-btn" data-id="${f.id}" title="Delete File">
                <i class="bi bi-trash"></i>
              </button>
            </div>
          </td>
        </tr>
      `;
    }).join("");

    attachRowActions();
  }

  function attachRowActions() {
    document.querySelectorAll(".inspect-btn").forEach(btn => {
      btn.onclick = () => inspectRoute(btn.getAttribute("data-id"));
    });

    document.querySelectorAll(".preview-btn").forEach(btn => {
      btn.onclick = () => previewFile(
        btn.getAttribute("data-id"),
        btn.getAttribute("data-name"),
        btn.getAttribute("data-type")
      );
    });

    document.querySelectorAll(".download-btn").forEach(btn => {
      btn.onclick = () => downloadFile(btn.getAttribute("data-id"));
    });

    document.querySelectorAll(".delete-btn").forEach(btn => {
      btn.onclick = () => deleteFile(btn.getAttribute("data-id"));
    });
  }

  // 3. Inspect Route (Geolocation Routing Diagnostics Modal)
  // Displays only Client Location and Target Storage Location (NO IP shown)
  async function inspectRoute(id) {
    if (locationModal) locationModal.show();
    locationModalBody.innerHTML = `<p class="text-muted"><i class="bi bi-hourglass-split me-1"></i>Calculating distance metrics...</p>`;

    try {
      const country = window.realClientCountry || "KH";
      const url = `${API_ENDPOINT}/files/${id}/location?country=${encodeURIComponent(country)}`;
      const headers = { "X-Test-Country": country };

      const res = await fetch(url, { headers });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();

      const distanceRows = Object.entries(data.distances || {})
        .sort((a, b) => a[1] - b[1])
        .map(([loc, dist]) => {
        const isNearest = loc.toLowerCase() === (data.nearestStorage || "").toLowerCase();
        const available = (data.availableReplicas || []).some(r => r.toLowerCase() === loc.toLowerCase());
        return `
          <tr class="${isNearest ? "table-active fw-bold" : ""}">
            <td>${formatReplicaLocation(loc)} ${isNearest ? '<span class="badge bg-black text-white ms-1">Nearest</span>' : ""}</td>
            <td>${dist.toFixed(1)} km</td>
            <td>${available ? '<span class="text-dark fw-semibold"><i class="bi bi-check-circle me-1"></i>Available</span>' : '<span class="text-muted"><i class="bi bi-dash-circle me-1"></i>Not on node</span>'}</td>
          </tr>
        `;
      }).join("");

      locationModalBody.innerHTML = `
        <div class="mb-3 small">
          <p class="mb-1"><strong>File ID:</strong> <code class="file-code-id">${data.fileId}</code></p>
          <p class="mb-1"><strong>Client Location:</strong> <span class="fw-bold">${escapeHtml(data.clientCountry || window.realClientLocationName)}</span></p>
          <p class="mb-0"><strong>File Storage Location:</strong> <span class="badge bg-black text-white">${formatReplicaLocation(data.nearestStorage)}</span></p>
        </div>
        <div class="fw-bold small text-uppercase mb-2">Distance to Storage Nodes:</div>
        <div class="table-responsive">
          <table class="table table-sm table-bordered align-middle mb-2">
            <thead class="table-light">
              <tr>
                <th>Storage Node</th>
                <th>Distance</th>
                <th>Replica Status</th>
              </tr>
            </thead>
            <tbody>
              ${distanceRows || "<tr><td colspan='3' class='text-muted'>No distances recorded</td></tr>"}
            </tbody>
          </table>
        </div>
        <small class="text-muted d-block">
          The Central API automatically routes requests to the nearest online storage node. If that node becomes unavailable, it fails over to the next closest replica.
        </small>
      `;
    } catch (err) {
      locationModalBody.innerHTML = `
        <div class="alert alert-danger mb-0">
          <i class="bi bi-exclamation-triangle me-1"></i>Failed to fetch route diagnostics: ${escapeHtml(err.message)}
        </div>`;
    }
  }

  // 4. Download File
  function downloadFile(id) {
    const country = window.realClientCountry || "KH";
    const url = `${API_ENDPOINT}/files/${id}?country=${encodeURIComponent(country)}`;
    window.open(url, "_blank");
  }

  // 5. Preview File
  async function previewFile(id, name, type) {
    if (previewModal) previewModal.show();
    previewTitle.textContent = `Preview: ${name}`;
    previewBody.innerHTML = `<p class="text-muted"><i class="bi bi-hourglass-split me-1"></i>Loading preview...</p>`;

    const country = window.realClientCountry || "KH";
    let fileUrl = `${API_ENDPOINT}/files/${id}?country=${encodeURIComponent(country)}`;
    previewDownloadLink.href = fileUrl;

    if (type.startsWith("image/")) {
      previewBody.innerHTML = `<img src="${fileUrl}" class="preview-img" alt="${escapeHtml(name)}">`;
    } else if (type.startsWith("text/") || type.includes("json") || type.includes("xml")) {
      try {
        const res = await fetch(fileUrl);
        const text = await res.text();
        previewBody.innerHTML = `<pre class="preview-text">${escapeHtml(text)}</pre>`;
      } catch (err) {
        previewBody.innerHTML = `<div class="alert alert-danger mb-0"><i class="bi bi-exclamation-triangle me-1"></i>Failed to load preview: ${escapeHtml(err.message)}</div>`;
      }
    } else if (type === "application/pdf") {
      previewBody.innerHTML = `<iframe src="${fileUrl}" style="width: 100%; height: 60vh; border: 1px solid #e5e7eb; border-radius: 4px;"></iframe>`;
    } else {
      previewBody.innerHTML = `
        <div class="py-5 text-muted">
          <i class="bi bi-file-earmark display-4 d-block mb-3"></i>
          <p class="mb-0">Direct in-browser preview not supported for <code>${escapeHtml(type || "binary")}</code>.</p>
          <small>Use the Download button below to inspect the file.</small>
        </div>`;
    }
  }

  // 6. Delete File (Red button action)
  async function deleteFile(id) {
    const row = document.querySelector(`tr[data-id="${id}"]`);
    const fileName = row ? row.getAttribute("data-name") : id;

    if (!confirm(`Are you sure you want to delete file "${fileName}"?\nThis removes it from all storage nodes and MongoDB.`)) {
      return;
    }

    try {
      const res = await fetch(`/delete/${encodeURIComponent(id)}`, { method: "POST" });
      if (!res.ok) {
        const directRes = await fetch(`${API_ENDPOINT}/files/${id}`, { method: "DELETE" });
        if (!directRes.ok) throw new Error(`HTTP ${directRes.status}`);
      }
      showFeedback(`File "${fileName}" deleted successfully.`, "success");
      fetchFiles();
    } catch (err) {
      showFeedback(`Failed to delete file: ${err.message}`, "danger");
    }
  }

  // 7. Drag-and-Drop and File Upload Handlers
  if (browseBtn && fileInput) {
    browseBtn.addEventListener("click", () => fileInput.click());
    fileInput.addEventListener("change", (e) => {
      if (e.target.files && e.target.files[0]) {
        handleFileSelection(e.target.files[0]);
      }
    });
  }

  if (dropzone) {
    ["dragenter", "dragover"].forEach(evt => {
      dropzone.addEventListener(evt, (e) => {
        e.preventDefault();
        dropzone.classList.add("dragover");
      });
    });

    ["dragleave", "drop"].forEach(evt => {
      dropzone.addEventListener(evt, (e) => {
        e.preventDefault();
        dropzone.classList.remove("dragover");
      });
    });

    dropzone.addEventListener("drop", (e) => {
      if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0]) {
        handleFileSelection(e.dataTransfer.files[0]);
      }
    });
  }

  function handleFileSelection(file) {
    selectedFile = file;
    if (selectedFileName) selectedFileName.textContent = file.name;
    if (selectedFileSize) selectedFileSize.textContent = `(${formatBytes(file.size)})`;
    if (selectedFileInfo) selectedFileInfo.classList.remove("d-none");
    if (uploadFeedback) uploadFeedback.classList.add("d-none");
  }

  if (cancelSelectBtn) {
    cancelSelectBtn.addEventListener("click", () => {
      selectedFile = null;
      if (fileInput) fileInput.value = "";
      if (selectedFileInfo) selectedFileInfo.classList.add("d-none");
    });
  }

  // Upload confirmation: BLUE button
  if (uploadBtn) {
    uploadBtn.addEventListener("click", async () => {
      if (!selectedFile) return;

      uploadBtn.disabled = true;
      uploadBtn.innerHTML = `<span class="spinner-border spinner-border-sm me-1"></span>Uploading...`;

      const formData = new FormData();
      formData.append("file", selectedFile);

      try {
        const res = await fetch("/upload", {
          method: "POST",
          body: formData,
        });

        const data = await res.json().catch(() => ({}));

        if (!res.ok) {
          throw new Error(data.error || `HTTP ${res.status}`);
        }

        const repCount = (data.replication && data.replication.successfulReplicas) ? data.replication.successfulReplicas.length : 1;
        const repNodes = (data.replication && data.replication.successfulReplicas)
          ? data.replication.successfulReplicas.map(r => formatReplicaLocation(r)).join(", ")
          : "SINGAPORE";

        showFeedback(`File <strong>${escapeHtml(selectedFile.name)}</strong> uploaded and replicated across ${repCount} node(s): <strong>${repNodes}</strong>.`, "success");

        selectedFile = null;
        if (fileInput) fileInput.value = "";
        if (selectedFileInfo) selectedFileInfo.classList.add("d-none");
        fetchFiles();
      } catch (err) {
        showFeedback(`Upload failed: ${escapeHtml(err.message)}`, "danger");
      } finally {
        uploadBtn.disabled = false;
        uploadBtn.innerHTML = `<i class="bi bi-cloud-upload me-1"></i>Upload & Replicate`;
      }
    });
  }

  function showFeedback(msg, type) {
    if (!uploadFeedback) return;
    uploadFeedback.className = `alert alert-${type} mt-3 mb-0 small`;
    uploadFeedback.innerHTML = msg;
    uploadFeedback.classList.remove("d-none");
  }

  // Search filter
  if (searchInput) {
    searchInput.addEventListener("input", (e) => {
      const q = e.target.value.toLowerCase().trim();
      const filtered = allFiles.filter(f =>
        f.originalName.toLowerCase().includes(q) || f.id.toLowerCase().includes(q)
      );
      renderFiles(filtered);
    });
  }

  // =========================================================================
  // STORAGE WORKER NODES MANAGEMENT (Directly on Home Page)
  // =========================================================================

  async function fetchNodes() {
    if (!nodesTableBody) return;
    try {
      const res = await fetch(`${API_ENDPOINT}/nodes`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      const nodes = data.nodes || [];

      if (nodeCountBadge) nodeCountBadge.textContent = `${nodes.length} worker(s) registered`;

      if (nodes.length === 0) {
        nodesTableBody.innerHTML = `
          <tr>
            <td colspan="8" class="text-center py-4 text-muted">
              No storage workers currently registered. Click <strong>Add Storage Node</strong> above to register a worker.
            </td>
          </tr>`;
        return;
      }

      nodesTableBody.innerHTML = nodes.map(node => {
        let statusBadge = "bg-secondary";
        if (node.status === "ONLINE") statusBadge = "bg-dark";
        else if (node.status === "SYNCING") statusBadge = "bg-warning text-dark";
        else if (node.status === "OFFLINE" || node.status === "ERROR") statusBadge = "bg-danger";

        const coords = (node.latitude || node.longitude)
          ? `${node.latitude.toFixed(2)}, ${node.longitude.toFixed(2)}`
          : "—";

        const hb = node.lastHeartbeat ? new Date(node.lastHeartbeat).toLocaleTimeString() : "Never";

        return `
          <tr>
            <td class="ps-3 fw-bold font-monospace small">${escapeHtml(node.nodeId || node.id)}</td>
            <td class="font-monospace small">${escapeHtml(node.ip)}:${node.port}</td>
            <td>${escapeHtml(node.region || "—")}</td>
            <td>${escapeHtml(node.country || "—")}</td>
            <td class="small text-muted">${coords}</td>
            <td><span class="badge ${statusBadge} px-2 py-1">${escapeHtml(node.status)}</span></td>
            <td class="small text-muted">${hb}</td>
            <td class="text-end pe-3">
              <button class="btn btn-sm btn-outline-dark me-1" onclick="probeNodeHealth('${escapeHtml(node.id)}')">
                <i class="bi bi-activity me-1"></i>Health
              </button>
              <button class="btn btn-sm btn-danger" onclick="removeNode('${escapeHtml(node.id)}', '${escapeHtml(node.nodeId)}')">
                <i class="bi bi-trash3"></i>
              </button>
            </td>
          </tr>`;
      }).join("");

    } catch (err) {
      nodesTableBody.innerHTML = `
        <tr>
          <td colspan="8" class="text-center py-3 text-danger">
            <i class="bi bi-exclamation-triangle me-1"></i>Failed to load storage workers: ${escapeHtml(err.message)}
          </td>
        </tr>`;
      if (nodeCountBadge) nodeCountBadge.textContent = "Error";
    }
  }

  // State tracking for Add Node modal/progress
  let activeAddNodeAbortCtrl = null;
  let activeAddNodeTimeouts = [];

  function clearAddNodeTimers() {
    activeAddNodeTimeouts.forEach(t => clearTimeout(t));
    activeAddNodeTimeouts = [];
    if (activeAddNodeAbortCtrl) {
      activeAddNodeAbortCtrl.abort();
      activeAddNodeAbortCtrl = null;
    }
  }

  function cancelAndResetAddNode() {
    // 1. Abort any running fetch and timers
    clearAddNodeTimers();

    // 2. Hide collapse
    if (addNodeCollapse) {
      addNodeCollapse.classList.add("d-none");
    }

    // 3. Reset and hide progress box and alert
    if (homeProgressBox) {
      homeProgressBox.classList.add("d-none");
    }
    if (homeProgressAlert) {
      homeProgressAlert.classList.add("d-none");
      homeProgressAlert.innerHTML = "";
    }
    if (homeProgressSpinner) {
      homeProgressSpinner.classList.add("d-none");
    }

    // 4. Reset step list to initial unstarted state
    const defaultTexts = [
      "Step 1: Contacting storage worker health endpoint...",
      "Step 2: Verifying worker identity & status...",
      "Step 3: Registering node as PENDING in MongoDB...",
      "Step 4: Synchronizing existing files via rsync...",
      "Step 5: Activating worker node to ONLINE."
    ];
    for (let i = 1; i <= 5; i++) {
      const el = document.getElementById("hstep" + i);
      if (el) {
        el.className = "mb-1 text-muted";
        el.innerHTML = `<i class="bi bi-circle text-muted me-2"></i>${defaultTexts[i - 1]}`;
      }
    }

    // 5. Reset form inputs
    if (homeAddNodeForm) {
      homeAddNodeForm.reset();
      const portInput = document.getElementById("homeNodePort");
      if (portInput) portInput.value = "8081";
    }

    // 6. Re-enable submit button
    if (homeSubmitNodeBtn) {
      homeSubmitNodeBtn.disabled = false;
    }
  }

  // Toggle inline Add Node form on Home Page
  if (toggleAddNodeBtn && addNodeCollapse) {
    toggleAddNodeBtn.addEventListener("click", () => {
      if (addNodeCollapse.classList.contains("d-none")) {
        // Opening form fresh
        cancelAndResetAddNode();
        addNodeCollapse.classList.remove("d-none");
      } else {
        // Closing form
        cancelAndResetAddNode();
      }
    });
  }
  if (closeAddNodeBtn) {
    closeAddNodeBtn.addEventListener("click", () => {
      cancelAndResetAddNode();
    });
  }
  if (cancelHomeAddNodeBtn) {
    cancelHomeAddNodeBtn.addEventListener("click", () => {
      cancelAndResetAddNode();
    });
  }

  // Preset button helper
  window.fillHomePreset = function (ip, port, region, country, lat, lon) {
    // Clear any previous error box when user selects a preset
    if (homeProgressBox) homeProgressBox.classList.add("d-none");
    if (homeProgressAlert) homeProgressAlert.classList.add("d-none");
    if (homeProgressSpinner) homeProgressSpinner.classList.add("d-none");
    if (homeSubmitNodeBtn) homeSubmitNodeBtn.disabled = false;

    document.getElementById("homeNodeIP").value = ip;
    document.getElementById("homeNodePort").value = port;
    document.getElementById("homeNodeRegion").value = region;
    document.getElementById("homeNodeCountry").value = country;
    document.getElementById("homeNodeLat").value = lat;
    document.getElementById("homeNodeLon").value = lon;
  };

  // Auto-detect Location, Latitude & Longitude from IP or Country
  const btnAutoGeo = document.getElementById("btnAutoGeo");
  if (btnAutoGeo) {
    btnAutoGeo.addEventListener("click", async () => {
      const ip = (document.getElementById("homeNodeIP")?.value || "").trim();
      if (!ip) {
        alert("Please enter an IP address or hostname first.");
        return;
      }

      btnAutoGeo.disabled = true;
      btnAutoGeo.innerHTML = `<span class="spinner-border spinner-border-sm"></span>`;

      try {
        // Check local presets first
        if (ip === "storage-new-node" || ip === "198.45.98.44") {
          document.getElementById("homeNodeRegion").value = "Asia";
          document.getElementById("homeNodeCountry").value = "Japan";
          document.getElementById("homeNodeLat").value = "35.6762";
          document.getElementById("homeNodeLon").value = "139.6503";
          const info = document.getElementById("geoDetectedInfo");
          if (info) info.innerHTML = `<i class="bi bi-check-circle text-success me-1"></i>Detected: <strong>Japan (Asia)</strong> [35.6762, 139.6503]`;
          return;
        }

        // Query public GeoIP
        const res = await fetch(`https://ipapi.co/${encodeURIComponent(ip)}/json/`, { signal: AbortSignal.timeout(4000) });
        if (!res.ok) throw new Error("GeoIP lookup failed");
        const data = await res.json();
        if (data.error) throw new Error(data.reason || "Invalid IP address");

        if (data.country_name) document.getElementById("homeNodeCountry").value = data.country_name;
        if (data.region || data.city) document.getElementById("homeNodeRegion").value = data.city || data.region;
        if (typeof data.latitude === "number") document.getElementById("homeNodeLat").value = data.latitude.toFixed(4);
        if (typeof data.longitude === "number") document.getElementById("homeNodeLon").value = data.longitude.toFixed(4);

        const info = document.getElementById("geoDetectedInfo");
        if (info) info.innerHTML = `<i class="bi bi-check-circle text-success me-1"></i>Detected: <strong>${escapeHtml(data.country_name || "")}</strong> (${data.latitude.toFixed(2)}, ${data.longitude.toFixed(2)})`;
      } catch (err) {
        // Fallback: check if country field has a known country name
        const countryVal = (document.getElementById("homeNodeCountry")?.value || document.getElementById("homeNodeRegion")?.value || "").toLowerCase().replace(/\s+/g, "");
        const dict = {
          singapore: { lat: 1.3521, lon: 103.8198, c: "Singapore", r: "Singapore" },
          japan: { lat: 35.6762, lon: 139.6503, c: "Japan", r: "Tokyo" },
          tokyo: { lat: 35.6762, lon: 139.6503, c: "Japan", r: "Tokyo" },
          london: { lat: 51.5074, lon: -0.1278, c: "United Kingdom", r: "London" },
          uk: { lat: 51.5074, lon: -0.1278, c: "United Kingdom", r: "London" },
          unitedkingdom: { lat: 51.5074, lon: -0.1278, c: "United Kingdom", r: "London" },
          usa: { lat: 38.9072, lon: -77.0369, c: "United States", r: "Washington" },
          unitedstates: { lat: 38.9072, lon: -77.0369, c: "United States", r: "Washington" },
          newyork: { lat: 40.7128, lon: -74.0060, c: "United States", r: "New York" },
          germany: { lat: 50.1109, lon: 8.6821, c: "Germany", r: "Frankfurt" },
          cambodia: { lat: 11.5564, lon: 104.9282, c: "Cambodia", r: "Phnom Penh" },
        };
        if (dict[countryVal]) {
          const match = dict[countryVal];
          document.getElementById("homeNodeCountry").value = match.c;
          document.getElementById("homeNodeRegion").value = match.r;
          document.getElementById("homeNodeLat").value = match.lat;
          document.getElementById("homeNodeLon").value = match.lon;
          const info = document.getElementById("geoDetectedInfo");
          if (info) info.innerHTML = `<i class="bi bi-check-circle text-success me-1"></i>Preset: <strong>${match.c}</strong>`;
        }
      } finally {
        btnAutoGeo.disabled = false;
        btnAutoGeo.innerHTML = `<i class="bi bi-geo-alt me-1"></i>Auto`;
      }
    });
  }

  // Auto-detect on IP blur
  const ipInput = document.getElementById("homeNodeIP");
  if (ipInput) {
    ipInput.addEventListener("blur", () => {
      const val = ipInput.value.trim();
      if (val && !document.getElementById("homeNodeLat")?.value) {
        btnAutoGeo?.click();
      }
    });
  }

  // Submit Add Node Form from Home Page
  if (homeAddNodeForm) {
    homeAddNodeForm.addEventListener("submit", async (e) => {
      e.preventDefault();

      const ip = document.getElementById("homeNodeIP").value.trim();
      let port = parseInt(document.getElementById("homeNodePort")?.value, 10);
      if (isNaN(port) || port <= 0) {
        port = 80;
      }
      let region = (document.getElementById("homeNodeRegion")?.value || "").trim();
      let country = (document.getElementById("homeNodeCountry")?.value || "").trim();
      let lat = parseFloat(document.getElementById("homeNodeLat")?.value) || 0;
      let lon = parseFloat(document.getElementById("homeNodeLon")?.value) || 0;

      if (!ip) return;

      // Fast auto-detect before sending if coords not filled yet
      if (lat === 0 && lon === 0) {
        if (ip === "storage-new-node" || ip === "198.45.98.44") {
          region = region || "Asia";
          country = country || "Japan";
          lat = 35.6762;
          lon = 139.6503;
        } else {
          try {
            const geoRes = await fetch(`https://ipapi.co/${encodeURIComponent(ip)}/json/`, { signal: AbortSignal.timeout(2000) });
            if (geoRes.ok) {
              const geoData = await geoRes.json();
              if (geoData.latitude && geoData.longitude) {
                lat = parseFloat(geoData.latitude.toFixed(4));
                lon = parseFloat(geoData.longitude.toFixed(4));
                if (!country && geoData.country_name) country = geoData.country_name;
                if (!region && (geoData.city || geoData.region)) region = geoData.city || geoData.region;
              }
            }
          } catch (_) {}
        }
      }
      if (!region) region = country || "Asia";
      if (!country) country = region;

      clearAddNodeTimers();
      activeAddNodeAbortCtrl = new AbortController();

      homeSubmitNodeBtn.disabled = true;
      homeProgressBox.classList.remove("d-none");
      homeProgressAlert.classList.add("d-none");
      resetHomeSteps();

      setHomeStepActive("hstep1", `Step 1: Contacting storage worker at ${escapeHtml(ip)}:${port}...`);

      try {
        const payload = { ip, port, region, country, latitude: lat, longitude: lon };

        const t1 = setTimeout(() => {
          setHomeStepDone("hstep1", "Step 1: Contacted storage worker.");
          setHomeStepActive("hstep2", "Step 2: Verifying worker identity & /health endpoint...");
        }, 400);
        activeAddNodeTimeouts.push(t1);

        const t2 = setTimeout(() => {
          setHomeStepDone("hstep2", "Step 2: Worker verified (valid storage service).");
          setHomeStepActive("hstep3", "Step 3: Registering PENDING node in MongoDB...");
        }, 900);
        activeAddNodeTimeouts.push(t2);

        const res = await fetch("/nodes/add", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
          signal: activeAddNodeAbortCtrl.signal,
        });

        const data = await res.json().catch(() => ({}));

        if (!res.ok) {
          clearAddNodeTimers();
          homeProgressSpinner.classList.add("d-none");
          const errMsg = data.error || `HTTP ${res.status}`;
          setHomeStepError("hstep2", "Worker health verification failed.");
          showHomeProgressAlert(`<strong>Node registration failed.</strong> Reason: ${escapeHtml(errMsg)}`, "danger");
          homeSubmitNodeBtn.disabled = false;
          return;
        }

        // Success sequence
        clearAddNodeTimers();
        setHomeStepDone("hstep1", "Step 1: Contacted storage worker.");
        setHomeStepDone("hstep2", "Step 2: Worker verified (valid storage service).");
        setHomeStepDone("hstep3", "Step 3: Node registered in MongoDB.");
        setHomeStepActive("hstep4", "Step 4: Synchronizing existing files via rsync...");

        const t3 = setTimeout(() => {
          setHomeStepDone("hstep4", "Step 4: File synchronization complete.");
          setHomeStepDone("hstep5", "Step 5: Worker activated to ONLINE!");
          homeProgressSpinner.classList.add("d-none");

          showHomeProgressAlert(`<strong>Success!</strong> Node <code>${escapeHtml(data.node?.nodeId || ip)}</code> is now ONLINE!`, "success");

          const t4 = setTimeout(() => {
            cancelAndResetAddNode();
            fetchNodes();
          }, 2000);
          activeAddNodeTimeouts.push(t4);
        }, 600);
        activeAddNodeTimeouts.push(t3);

      } catch (err) {
        if (err.name === "AbortError") {
          // Request cancelled by user
          return;
        }
        clearAddNodeTimers();
        homeProgressSpinner.classList.add("d-none");
        setHomeStepError("hstep1", "Network error contacting Central API.");
        showHomeProgressAlert(`<strong>Error:</strong> ${escapeHtml(err.message)}`, "danger");
        homeSubmitNodeBtn.disabled = false;
      }
    });
  }

  function resetHomeSteps() {
    homeProgressSpinner.classList.remove("d-none");
    for (let i = 1; i <= 5; i++) {
      const el = document.getElementById("hstep" + i);
      if (el) {
        el.className = "mb-1 text-muted";
        el.innerHTML = `<i class="bi bi-circle text-muted me-2"></i>Step ${i}`;
      }
    }
  }

  function setHomeStepActive(id, text) {
    const el = document.getElementById(id);
    if (!el) return;
    el.className = "mb-1 fw-semibold text-primary";
    el.innerHTML = `<span class="spinner-border spinner-border-sm me-2"></span>${text}`;
  }

  function setHomeStepDone(id, text) {
    const el = document.getElementById(id);
    if (!el) return;
    el.className = "mb-1 text-success";
    el.innerHTML = `<i class="bi bi-check-circle-fill text-success me-2"></i>${text}`;
  }

  function setHomeStepError(id, text) {
    const el = document.getElementById(id);
    if (!el) return;
    el.className = "mb-1 text-danger";
    el.innerHTML = `<i class="bi bi-x-circle-fill text-danger me-2"></i>${text}`;
  }

  function showHomeProgressAlert(html, type) {
    homeProgressAlert.className = `alert alert-${type} mt-2 mb-0 small`;
    homeProgressAlert.innerHTML = html;
    homeProgressAlert.classList.remove("d-none");
  }

  // Global Node actions
  window.probeNodeHealth = async function (id) {
    showNodeAlert("Probing worker health...", "info");
    try {
      const res = await fetch(`${API_ENDPOINT}/nodes/${encodeURIComponent(id)}/health`);
      const data = await res.json();
      if (res.ok && data.success) {
        showNodeAlert(`Node ${data.node?.nodeId || id} is healthy and ONLINE.`, "success");
      } else {
        showNodeAlert(`Node ${id} probe: ${data.status || "OFFLINE"} (${data.error || "unresponsive"})`, "warning");
      }
      fetchNodes();
    } catch (err) {
      showNodeAlert(`Health check error: ${err.message}`, "danger");
    }
  };

  window.removeNode = async function (id, nodeId) {
    const displayName = nodeId || id;
    if (!confirm(`Are you sure you want to remove storage node "${displayName}"?\nThis node will be deactivated and no longer serve files.`)) {
      return;
    }

    showNodeAlert(`Removing node ${displayName}...`, "info");
    try {
      const res = await fetch(`${API_ENDPOINT}/nodes/${encodeURIComponent(id)}`, { method: "DELETE" });
      if (!res.ok) {
        const errData = await res.json().catch(() => ({}));
        throw new Error(errData.error || `HTTP ${res.status}`);
      }
      showNodeAlert(`Storage worker "${displayName}" removed successfully.`, "success");
      fetchNodes();
    } catch (err) {
      showNodeAlert(`Failed to remove node: ${err.message}`, "danger");
    }
  };

  function showNodeAlert(msg, type) {
    if (!nodeActionAlert) return;
    nodeActionAlert.className = `alert alert-${type} small mx-3 mt-3 mb-0 py-2`;
    nodeActionAlert.innerHTML = msg;
    nodeActionAlert.classList.remove("d-none");
    setTimeout(() => {
      nodeActionAlert.classList.add("d-none");
    }, 5000);
  }

  // Refresh Button
  if (refreshBtn) {
    refreshBtn.addEventListener("click", () => {
      fetchFiles();
      fetchNodes();
    });
  }

  // Initial load
  detectRealClientLocation();
  fetchFiles();
  fetchNodes();
});
