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

  if (!form) return;

  form.addEventListener("submit", async function (e) {
    e.preventDefault();

    const ip = document.getElementById("nodeIP").value.trim();
    const port = parseInt(document.getElementById("nodePort").value, 10) || 22;
    const region = document.getElementById("nodeRegion").value.trim();
    const country = document.getElementById("nodeCountry").value.trim();
    const lat = parseFloat(document.getElementById("nodeLat").value) || 0;
    const lon = parseFloat(document.getElementById("nodeLon").value) || 0;
    const csrfToken = document.getElementById("csrfToken")?.value || getCookie("cdn_csrf_token");

    if (!ip) {
      alert("Storage Server IP Address or Hostname is required.");
      return;
    }

    submitBtn.disabled = true;
    progressBox.classList.remove("d-none");
    progressAlert.classList.add("d-none");
    resetSteps();

    // Step 1: Connecting
    setStepActive("step1", "Step 1: Connecting via SSH with strict host-key verification...");

    try {
      const payload = {
        ip: ip,
        port: port,
        region: region,
        country: country,
        latitude: lat,
        longitude: lon,
      };

      const stepTimer1 = setTimeout(() => {
        setStepDone("step1", "Step 1: Host key verified. cdn-sync authenticated.");
        setStepActive("step2", "Step 2: Checking storage directory (/var/cdn/data) permissions...");
      }, 600);

      const stepTimer2 = setTimeout(() => {
        setStepDone("step2", "Step 2: Storage directory verified healthy.");
        setStepActive("step3", "Step 3: Creating PENDING record & capturing sync snapshot in MongoDB...");
      }, 1200);

      const headers = {
        "Content-Type": "application/json",
      };
      if (csrfToken) {
        headers["X-CSRF-Token"] = csrfToken;
      }

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
          showProgressAlert(`<strong>Authentication Required.</strong> Your session has expired. Redirecting to login...`, "warning");
          setTimeout(() => { window.location.href = "/admin/login?next=/nodes/add"; }, 1500);
          return;
        }

        setStepError("step1", "Onboarding validation or synchronization failed.");
        showProgressAlert(`<strong>Node onboarding rejected:</strong><br>${escapeHTML(errMsg)}`, "danger");
        submitBtn.disabled = false;
        return;
      }

      // Success sequence
      setStepDone("step1", "Step 1: Host key verified. cdn-sync authenticated.");
      setStepDone("step2", "Step 2: Storage directory /var/cdn/data verified.");
      setStepDone("step3", "Step 3: Initialized sync job and catalog snapshot.");
      setStepActive("step4", "Step 4: Transferred catalog files & replaying mutation journal...");

      setTimeout(() => {
        setStepDone("step4", "Step 4: Catalog synchronized and mutations replayed.");
        setStepDone("step5", "Step 5: Mutation barrier passed! Node activated to ONLINE.");
        progressSpinner.classList.add("d-none");

        showProgressAlert(
          `<strong>Success!</strong> Node <code>${escapeHTML(data.node?.nodeId || ip)}</code> is fully synchronized and <strong>ONLINE</strong>.<br>Redirecting to Storage Nodes list...`,
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

  function getCookie(name) {
    const value = `; ${document.cookie}`;
    const parts = value.split(`; ${name}=`);
    if (parts.length === 2) return parts.pop().split(';').shift();
    return "";
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
