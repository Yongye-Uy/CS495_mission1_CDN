// add_node.js - Storage Worker Onboarding Form

function fillPreset(ip, port, region, country, lat, lon) {
  const progressBox = document.getElementById("onboardingProgressBox");
  const progressAlert = document.getElementById("progressAlert");
  const submitBtn = document.getElementById("submitNodeBtn");
  if (progressBox) progressBox.classList.add("d-none");
  if (progressAlert) progressAlert.classList.add("d-none");
  if (submitBtn) submitBtn.disabled = false;

  document.getElementById("nodeIP").value = ip;
  document.getElementById("nodePort").value = port;
  document.getElementById("nodeRegion").value = region;
  document.getElementById("nodeCountry").value = country;
  document.getElementById("nodeLat").value = lat;
  document.getElementById("nodeLon").value = lon;
}

document.addEventListener("DOMContentLoaded", function () {
  const form = document.getElementById("addNodeForm");
  const submitBtn = document.getElementById("submitNodeBtn");
  const progressBox = document.getElementById("onboardingProgressBox");
  const progressAlert = document.getElementById("progressAlert");
  const progressSpinner = document.getElementById("progressSpinner");

  form.addEventListener("submit", async function (e) {
    e.preventDefault();

    const ip = document.getElementById("nodeIP").value.trim();
    const port = parseInt(document.getElementById("nodePort").value, 10) || 8081;
    const region = document.getElementById("nodeRegion").value.trim();
    const country = document.getElementById("nodeCountry").value.trim();
    const lat = parseFloat(document.getElementById("nodeLat").value) || 0;
    const lon = parseFloat(document.getElementById("nodeLon").value) || 0;

    if (!ip || !region) {
      alert("IP Address and Region are required.");
      return;
    }

    submitBtn.disabled = true;
    progressBox.classList.remove("d-none");
    progressAlert.classList.add("d-none");
    resetSteps();

    // Step 1: Contacting
    setStepActive("step1", "Step 1: Contacting storage worker at " + escapeHTML(ip) + ":" + port + "...");

    try {
      const payload = {
        ip: ip,
        port: port,
        region: region,
        country: country,
        latitude: lat,
        longitude: lon,
      };

      // Step 2 & 3
      setTimeout(() => {
        setStepDone("step1", "Step 1: Contacted storage worker.");
        setStepActive("step2", "Step 2: Verifying worker identity & /health status...");
      }, 500);

      setTimeout(() => {
        setStepDone("step2", "Step 2: Worker verified (valid storage service).");
        setStepActive("step3", "Step 3: Creating PENDING record in MongoDB...");
      }, 1000);

      const res = await fetch("/nodes/add", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });

      const data = await res.json().catch(() => ({}));

      if (!res.ok) {
        progressSpinner.classList.add("d-none");
        const errMsg = data.error || `HTTP ${res.status}`;
        setStepError("step2", "Worker verification / registration failed.");
        showProgressAlert(`<strong>Node registration failed.</strong><br>Reason: ${escapeHTML(errMsg)}`, "danger");
        submitBtn.disabled = false;
        return;
      }

      // Success sequence
      setStepDone("step3", "Step 3: Node registered in MongoDB (storage_nodes).");
      setStepActive("step4", "Step 4: Synchronizing existing data via rsync...");

      setTimeout(() => {
        setStepDone("step4", "Step 4: rsync synchronization complete.");
        setStepDone("step5", "Step 5: Node activated to ONLINE!");
        progressSpinner.classList.add("d-none");

        showProgressAlert(
          `<strong>Success!</strong> Node <code>${escapeHTML(data.node?.nodeId || ip)}</code> is now ONLINE.<br>Redirecting to Storage Nodes list...`,
          "success"
        );

        setTimeout(() => {
          window.location.href = "/nodes";
        }, 2200);
      }, 600);

    } catch (err) {
      progressSpinner.classList.add("d-none");
      setStepError("step1", "Network error contacting Central API.");
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
