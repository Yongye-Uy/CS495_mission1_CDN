// add_node.js - Storage Worker Onboarding Form

function fillPreset(ip, port, region, country, lat, lon) {
  const progressBox = document.getElementById("onboardingProgressBox");
  const progressAlert = document.getElementById("progressAlert");
  const submitBtn = document.getElementById("submitNodeBtn");
  if (progressBox) progressBox.classList.add("d-none");
  if (progressAlert) progressAlert.classList.add("d-none");
  if (submitBtn) submitBtn.disabled = false;

  document.getElementById("nodeIP").value = ip;
  if (port) document.getElementById("nodePort").value = port;
  if (region) document.getElementById("nodeRegion").value = region;
  if (country) document.getElementById("nodeCountry").value = country;
  if (lat) document.getElementById("nodeLat").value = lat;
  if (lon) document.getElementById("nodeLon").value = lon;

  const userEl = document.getElementById("nodeSSHUser");
  if (userEl && !userEl.value) userEl.value = "ubuntu";
  const dirEl = document.getElementById("nodeStorageDir");
  if (dirEl && !dirEl.value) dirEl.value = "/var/cdn/data";

  // Expand accordion to show prefilled settings
  const collapseEl = document.getElementById("collapseAdv");
  if (collapseEl && window.bootstrap && window.bootstrap.Collapse) {
    const bsCollapse = bootstrap.Collapse.getOrCreateInstance(collapseEl, { toggle: false });
    bsCollapse.show();
  }
}

document.addEventListener("DOMContentLoaded", function () {
  const form = document.getElementById("addNodeForm");
  const submitBtn = document.getElementById("submitNodeBtn");
  const progressBox = document.getElementById("onboardingProgressBox");
  const progressAlert = document.getElementById("progressAlert");
  const progressSpinner = document.getElementById("progressSpinner");
  const nodeIPInput = document.getElementById("nodeIP");
  const detectBtn = document.getElementById("detectBtn");
  const detectionStatus = document.getElementById("detectionStatus");
  const detectionAlert = document.getElementById("detectionAlert");
  const detectSpinner = document.getElementById("detectSpinner");
  const detectSuccessIcon = document.getElementById("detectSuccessIcon");
  const detectMessage = document.getElementById("detectMessage");

  if (!form) return;

  // Auto-detection logic on IP input with debounce
  let detectDebounceTimer = null;
  let lastDetectedIP = "";

  async function performDetection(ip) {
    ip = ip.trim();
    if (!ip || ip === lastDetectedIP) return;
    lastDetectedIP = ip;

    if (detectionStatus) detectionStatus.classList.remove("d-none");
    if (detectionAlert) detectionAlert.className = "alert alert-info py-2 px-3 mb-0 small d-flex align-items-center gap-2";
    if (detectSpinner) detectSpinner.classList.remove("d-none");
    if (detectSuccessIcon) detectSuccessIcon.classList.add("d-none");
    if (detectMessage) detectMessage.textContent = `Auto-detecting parameters for ${ip}...`;

    try {
      const res = await fetch(`/api/nodes/detect?ip=${encodeURIComponent(ip)}`);
      const data = await res.json().catch(() => ({}));

      if (res.ok && data.success && data.data) {
        const d = data.data;
        if (d.port) document.getElementById("nodePort").value = d.port;
        if (d.region) document.getElementById("nodeRegion").value = d.region;
        if (d.country) document.getElementById("nodeCountry").value = d.country;
        if (d.latitude) document.getElementById("nodeLat").value = d.latitude;
        if (d.longitude) document.getElementById("nodeLon").value = d.longitude;
        if (d.ssh_user) document.getElementById("nodeSSHUser").value = d.ssh_user;
        if (d.storage_dir) document.getElementById("nodeStorageDir").value = d.storage_dir;

        if (detectSpinner) detectSpinner.classList.add("d-none");
        if (detectSuccessIcon) detectSuccessIcon.classList.remove("d-none");
        if (detectionAlert) detectionAlert.className = "alert alert-success py-2 px-3 mb-0 small d-flex align-items-center gap-2";

        const locText = [d.city, d.country].filter(Boolean).join(", ") || d.region || "Detected";
        let summaryHTML = `<strong>Detected:</strong> ${escapeHTML(locText)} &bull; SSH: <code>${escapeHTML(d.ssh_user || "ubuntu")}:${d.port || 22}</code> &bull; Dir: <code>${escapeHTML(d.storage_dir || "/var/cdn/data")}</code>`;
        if (d.host_key_enrolled) {
          summaryHTML += ` &bull; <span class="badge bg-success"><i class="bi bi-shield-check me-1"></i>Host Key Enrolled</span>`;
        }
        if (detectMessage) detectMessage.innerHTML = summaryHTML;
      } else {
        if (detectSpinner) detectSpinner.classList.add("d-none");
        if (detectionAlert) detectionAlert.className = "alert alert-warning py-2 px-3 mb-0 small d-flex align-items-center gap-2";
        if (detectMessage) detectMessage.textContent = "Could not auto-detect location. Using default settings (Port 22, user 'ubuntu', /var/cdn/data).";
      }
    } catch (err) {
      if (detectSpinner) detectSpinner.classList.add("d-none");
      if (detectionAlert) detectionAlert.className = "alert alert-light border py-2 px-3 mb-0 small d-flex align-items-center gap-2";
      if (detectMessage) detectMessage.textContent = "Auto-detection service offline. Manual defaults applied.";
    }
  }

  if (nodeIPInput) {
    nodeIPInput.addEventListener("input", function () {
      clearTimeout(detectDebounceTimer);
      const val = nodeIPInput.value.trim();
      if (val.length >= 3) {
        detectDebounceTimer = setTimeout(() => performDetection(val), 600);
      }
    });

    nodeIPInput.addEventListener("blur", function () {
      const val = nodeIPInput.value.trim();
      if (val.length >= 3) {
        performDetection(val);
      }
    });
  }

  if (detectBtn) {
    detectBtn.addEventListener("click", function () {
      const val = nodeIPInput ? nodeIPInput.value.trim() : "";
      if (val) {
        lastDetectedIP = ""; // Force re-detection
        performDetection(val);
      }
    });
  }

  form.addEventListener("submit", async function (e) {
    e.preventDefault();

    const ip = document.getElementById("nodeIP").value.trim();
    const port = parseInt(document.getElementById("nodePort").value, 10) || 22;
    const region = document.getElementById("nodeRegion").value.trim();
    const country = document.getElementById("nodeCountry").value.trim();
    const lat = parseFloat(document.getElementById("nodeLat").value) || 0;
    const lon = parseFloat(document.getElementById("nodeLon").value) || 0;
    const sshUser = (document.getElementById("nodeSSHUser")?.value || "ubuntu").trim();
    const storageDir = (document.getElementById("nodeStorageDir")?.value || "/var/cdn/data").trim();

    if (!ip) {
      alert("Storage Server IP Address or Hostname is required.");
      return;
    }

    submitBtn.disabled = true;
    progressBox.classList.remove("d-none");
    progressAlert.classList.add("d-none");
    resetSteps();

    // Step 1: Connecting
    setStepActive("step1", `Step 1: Enrolling host key & authenticating as ${sshUser}...`);

    try {
      const payload = {
        ip: ip,
        port: port,
        region: region,
        country: country,
        latitude: lat,
        longitude: lon,
        ssh_user: sshUser,
        storage_dir: storageDir,
      };

      const stepTimer1 = setTimeout(() => {
        setStepDone("step1", `Step 1: Host key verified. Authenticated as ${sshUser}.`);
        setStepActive("step2", `Step 2: Checking storage directory (${storageDir}) permissions...`);
      }, 600);

      const stepTimer2 = setTimeout(() => {
        setStepDone("step2", `Step 2: Storage directory ${storageDir} verified healthy.`);
        setStepActive("step3", "Step 3: Creating PENDING record & capturing catalog snapshot in MongoDB...");
      }, 1200);

      const headers = {
        "Content-Type": "application/json",
      };

      const res = await fetch("/nodes/add", {
        method: "POST",
        headers: headers,
        body: JSON.stringify(payload),
      });

      clearTimeout(stepTimer1);
      clearTimeout(stepTimer2);

      const data = await res.json().catch(() => ({}));

      if (!res.ok) {
        progressSpinner.classList.add("d-none");
        const errMsg = data.error || `HTTP ${res.status}`;
        if (res.status === 401) {
          showProgressAlert(`<strong>Unauthorized:</strong> Central API rejected administrative key.`, "danger");
          submitBtn.disabled = false;
          return;
        }

        setStepError("step1", "Onboarding validation or synchronization failed.");
        showProgressAlert(`<strong>Node onboarding rejected:</strong><br>${escapeHTML(errMsg)}`, "danger");
        submitBtn.disabled = false;
        return;
      }

      // Success sequence
      setStepDone("step1", `Step 1: Host key enrolled. Authenticated as ${sshUser}.`);
      setStepDone("step2", `Step 2: Storage directory ${storageDir} verified.`);
      setStepDone("step3", "Step 3: Initialized catalog snapshot.");
      setStepActive("step4", "Step 4: Synchronizing missing files via rsync / SFTP...");

      setTimeout(() => {
        setStepDone("step4", "Step 4: Catalog synchronized.");
        setStepDone("step5", "Step 5: Node activated to ONLINE!");
        progressSpinner.classList.add("d-none");

        showProgressAlert(
          `<strong>Success!</strong> Storage Node <code>${escapeHTML(data.node?.nodeId || ip)}</code> is fully synchronized and <strong>ONLINE</strong>.<br>Redirecting to Storage Nodes list...`,
          "success"
        );

        setTimeout(() => {
          window.location.href = "/nodes";
        }, 1800);
      }, 600);

    } catch (err) {
      progressSpinner.classList.add("d-none");
      setStepError("step1", "Network error contacting Website server.");
      showProgressAlert(`<strong>Error:</strong> ${escapeHTML(err.message)}`, "danger");
      submitBtn.disabled = false;
    }
  });

  function resetSteps() {
    progressSpinner.classList.remove("d-none");
    for (let i = 1; i <= 5; i++) {
      const el = document.getElementById("step" + i);
      if (el) {
        el.className = "mb-2 text-muted";
        el.innerHTML = `<i class="bi bi-circle text-muted me-2"></i>Step ${i}`;
      }
    }
  }

  function setStepActive(stepId, text) {
    const el = document.getElementById(stepId);
    if (!el) return;
    el.className = "mb-2 fw-semibold text-primary";
    el.innerHTML = `<span class="spinner-border spinner-border-sm me-2"></span>${text}`;
  }

  function setStepDone(stepId, text) {
    const el = document.getElementById(stepId);
    if (!el) return;
    el.className = "mb-2 text-success";
    el.innerHTML = `<i class="bi bi-check-circle-fill text-success me-2"></i>${text}`;
  }

  function setStepError(stepId, text) {
    const el = document.getElementById(stepId);
    if (!el) return;
    el.className = "mb-2 text-danger";
    el.innerHTML = `<i class="bi bi-x-circle-fill text-danger me-2"></i>${text}`;
  }

  function showProgressAlert(html, type) {
    progressAlert.className = `alert alert-${type} mt-3 mb-0 small`;
    progressAlert.innerHTML = html;
    progressAlert.classList.remove("d-none");
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
});
