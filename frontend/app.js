// ============================================
// VEGA CLOUD v9.0.0 - Premium Frontend
// ============================================

var API = '/api';
var CONTAINER_REFRESH_INTERVAL = 3000; // 3 segundos

// State
// State
var currentUser = null;
var containers = [];
var selectedContainer = null;
var terminal = null;
var terminalSocket = null;
var timerIntervals = {};
var fitAddon = null;
var containerRefreshInterval = null;
var firewallContainerId = null; // Filter for firewall view
var pollInterval = null;
var lastPremiumStatus = null;
var lastBanStatus = false;
var cpuChart = null;
var memChart = null;
var currentPath = "/";
var editorPath = "";
var editorContainerId = null;

// DOM Elements
// DOM Elements
var authPage, dashboardPage, loginForm, registerForm, authTabs, containersGrid, noContainers, containerModal, toastContainer;

// Firewall Elements
var firewallModal, firewallTableBody, firewallLoading, firewallEmpty;

function initDOMElements() {
  authPage = document.getElementById('auth-page');
  dashboardPage = document.getElementById('dashboard-page');
  loginForm = document.getElementById('login-form');
  registerForm = document.getElementById('register-form');
  authTabs = document.querySelectorAll('.auth-tab');
  containersGrid = document.getElementById('containers-grid');
  noContainers = document.getElementById('no-containers');
  containerModal = document.getElementById('container-modal');
  toastContainer = document.getElementById('toast-container');

  firewallModal = document.getElementById('firewall-modal');
  firewallTableBody = document.querySelector('#firewall-table tbody');
  firewallLoading = document.getElementById('firewall-loading');
  firewallEmpty = document.getElementById('firewall-empty');
}

// ==========================================
// INITIALIZATION
// ==========================================

document.addEventListener('DOMContentLoaded', async () => {
  initDOMElements(); // Initialize DOM elements first
  setupAuthTabs();
  setupAuthForms();
  setupDashboard();
  setupSidebar();
  setupSliders();
  setupFirewall();
  setupModalTabs();
  setupMonitoring();
  await checkAuth();
});

// ==========================================
// AUTHENTICATION
// ==========================================

async function checkAuth() {
  try {
    const res = await fetch(`${API}/auth/me`, { credentials: 'include' });
    if (res.ok) {
      currentUser = await res.json();

      // Initialize Poll State
      lastPremiumStatus = currentUser.is_premium;
      lastBanStatus = currentUser.is_banned || false;

      showDashboard();
      startStatusPoller(); // Start polling
    } else {
      showAuth();
    }
  } catch (err) {
    console.error('CheckAuth Error:', err);
    // showToast('Erro de conexão ao verificar login', 'error'); // Optional: show to user
    showAuth();
  }
}

function startStatusPoller() {
  if (pollInterval) clearInterval(pollInterval);
  pollInterval = setInterval(async () => {
    if (!currentUser) return;
    try {
      const res = await fetch(`${API}/auth/me`);
      if (res.ok) {
        const updatedUser = await res.json();

        // Check Ban
        if (updatedUser.is_banned && !lastBanStatus) {
          alert(`🚫 SUA CONTA FOI BANIDA!\n\nMotivo: ${updatedUser.ban_reason || 'Violação dos termos'}`);
          logout();
          return;
        }

        // Check Premium Change
        if (updatedUser.is_premium !== lastPremiumStatus) {
          if (updatedUser.is_premium) {
            alert("🎉 PARABÉNS!\n\nSua conta foi atualizada para PREMIUM!\nAproveite 24 vCPUs, 16GB RAM e containers ilimitados!");
          } else {
            alert("ℹ️ STATUS ATUALIZADO\n\nSua conta agora é Free.");
          }
          lastPremiumStatus = updatedUser.is_premium;
          currentUser.is_premium = updatedUser.is_premium; // Update global state

          // Refresh dashboard to show correct badge if visible
          const usernameDisplay = document.getElementById('username-display');
          if (usernameDisplay) usernameDisplay.textContent = currentUser.username.charAt(0).toUpperCase(); /* trigger re-render if needed or just reload page */
          location.reload(); // Simplest way to refresh all UI elements like max sliders
          return;
        }

        lastBanStatus = updatedUser.is_banned;

      } else if (res.status === 401 || res.status === 403) {
        logout();
      }
    } catch (e) { console.error("Poll error", e); }
  }, 5000); // 5 seconds
}

function showAuth() {
  authPage.classList.add('active');
  dashboardPage.classList.remove('active');
}

function showDashboard() {
  authPage.classList.remove('active');
  dashboardPage.classList.add('active');

  // Update user display
  const usernameDisplay = document.getElementById('username-display');
  const userNameDisplay = document.getElementById('user-name-display');

  if (usernameDisplay) {
    usernameDisplay.textContent = currentUser.username.charAt(0).toUpperCase();
  }
  if (userNameDisplay) {
    userNameDisplay.textContent = currentUser.username;
  }
  updateUserRoleBadge();

  loadContainers();

  // Auto-refresh containers
  if (containerRefreshInterval) clearInterval(containerRefreshInterval);
  containerRefreshInterval = setInterval(() => {
    // Only refresh if showing containers view
    if (!document.getElementById('view-containers').classList.contains('hidden') && !selectedContainer) {
      loadContainers();
    }
  }, CONTAINER_REFRESH_INTERVAL);

  // Admin & User Separation Logic
  const adminNav = document.getElementById('nav-admin');
  const userNavContainers = document.getElementById('nav-containers');
  const userNavFirewall = document.getElementById('nav-firewall');
  const btnNewContainer = document.getElementById('btn-new-container');
  const btnCreateFirst = document.querySelector('.btn-create-first');

  if (currentUser && currentUser.is_admin === true) {
    // === ADMIN VIEW ===
    console.log('User is Admin. Enabling Admin Mode.');

    // Show Admin Nav
    if (adminNav) {
      adminNav.classList.remove('hidden');
      adminNav.style.display = 'flex';
    }

    // Hide User Navs
    if (userNavContainers) userNavContainers.style.display = 'none';
    if (userNavFirewall) userNavFirewall.style.display = 'none';

    // Hide Create Buttons
    if (btnNewContainer) btnNewContainer.style.display = 'none';
    if (btnCreateFirst) btnCreateFirst.style.display = 'none';

    // Force Admin View
    const adminView = document.getElementById('view-admin');
    if (adminView) {
      // Hide all other views
      document.getElementById('view-containers').classList.add('hidden');
      document.getElementById('view-firewall').classList.add('hidden');

      // Show Admin View
      adminView.classList.remove('hidden');
      loadAdminView();

      // Update Active Nav
      document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
      if (adminNav) adminNav.classList.add('active');
    }

  } else {
    // === STANDARD USER VIEW ===
    console.log('User is Standard. Enabling User Mode.');

    // Hide Admin Nav
    if (adminNav) {
      adminNav.classList.add('hidden');
      adminNav.style.display = 'none';
    }

    // Show User Navs
    if (userNavContainers) userNavContainers.style.display = 'flex';
    if (userNavFirewall) userNavFirewall.style.display = 'flex';

    // Show Create Buttons
    if (btnNewContainer) btnNewContainer.style.display = 'flex';
    // btnCreateFirst visibility depends on container count, handled by renderContainers

    // Ensure not on admin view
    if (!document.getElementById('view-admin').classList.contains('hidden')) {
      document.querySelector('[data-view="containers"]').click();
    }
  }
}

function updateUserRoleBadge() {
  const roleEl = document.querySelector('.user-role');
  if (!roleEl || !currentUser) return;

  if (currentUser.is_admin) {
    roleEl.textContent = 'Admin';
  } else if (currentUser.is_premium) {
    roleEl.textContent = 'Premium';
  } else {
    roleEl.textContent = 'Free Tier';
  }
}

function setupAuthTabs() {
  authTabs.forEach(tab => {
    tab.addEventListener('click', () => {
      authTabs.forEach(t => t.classList.remove('active'));
      tab.classList.add('active');

      const forms = document.querySelectorAll('.auth-form');
      forms.forEach(f => f.classList.remove('active'));

      const targetForm = document.getElementById(`${tab.dataset.tab}-form`);
      if (targetForm) targetForm.classList.add('active');
    });
  });
}

function setupAuthForms() {
  // Login Form
  loginForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    const btn = loginForm.querySelector('button[type="submit"]');
    const originalContent = btn.innerHTML;
    btn.disabled = true;
    btn.innerHTML = '<span class="spinner"></span><span>Entrando...</span>';

    try {
      const res = await fetch(`${API}/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username: document.getElementById('login-username').value.trim(),
          password: document.getElementById('login-password').value
        })
      });

      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Erro no login');

      currentUser = data;
      showToast('Login realizado com sucesso!', 'success');
      showDashboard();
    } catch (err) {
      showToast(err.message, 'error');
    } finally {
      btn.disabled = false;
      btn.innerHTML = originalContent;
    }
  });

  // Register Form
  registerForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    const btn = registerForm.querySelector('button[type="submit"]');
    const originalContent = btn.innerHTML;
    btn.disabled = true;
    btn.innerHTML = '<span class="spinner"></span><span>Criando...</span>';

    try {
      const res = await fetch(`${API}/auth/register`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username: document.getElementById('register-username').value.trim(),
          password: document.getElementById('register-password').value
        })
      });

      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Erro ao criar conta');

      currentUser = data;
      showToast('Conta criada com sucesso!', 'success');
      showDashboard();
    } catch (err) {
      showToast(err.message, 'error');
    } finally {
      btn.disabled = false;
      btn.innerHTML = originalContent;
    }
  });
}

// ==========================================
// DASHBOARD & NAVIGATION
// ==========================================

function setupDashboard() {
  document.getElementById('btn-logout').addEventListener('click', logout);
  document.getElementById('btn-new-container').addEventListener('click', showNewContainerModal);
  document.querySelector('.btn-create-first')?.addEventListener('click', showNewContainerModal);
  document.querySelector('.modal-backdrop')?.addEventListener('click', closeModal);
  document.getElementById('modal-btn-reset')?.addEventListener('click', () => resetTimer(selectedContainer?.id));
  document.getElementById('modal-btn-reset-big')?.addEventListener('click', () => resetTimer(selectedContainer?.id));
  document.getElementById('modal-btn-delete')?.addEventListener('click', () => deleteContainer(selectedContainer?.id));
}

function setupSidebar() {
  document.querySelectorAll('.nav-item').forEach(item => {
    item.addEventListener('click', (e) => {
      e.preventDefault();
      const view = item.dataset.view;
      if (!view) return; // Ignore non-view links

      // Update Nav
      document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
      item.classList.add('active');

      // Switch Views
      const containerView = document.getElementById('view-containers');
      const firewallView = document.getElementById('view-firewall');
      const adminView = document.getElementById('view-admin');

      containerView.classList.add('hidden');
      firewallView.classList.add('hidden');
      if (adminView) adminView.classList.add('hidden');

      if (view === 'containers') {
        containerView.classList.remove('hidden');
      } else if (view === 'firewall') {
        firewallView.classList.remove('hidden');
        containerView.classList.add('hidden'); // Ensure hidden
        loadFirewallView();
      } else if (view === 'admin') {
        if (!currentUser.is_admin) return;
        adminView.classList.remove('hidden');
        loadAdminView();
      }
    });
  });
}

async function logout() {
  await fetch(`${API}/auth/logout`, { method: 'POST' }).catch(() => { });
  currentUser = null;
  containers = [];
  Object.values(timerIntervals).forEach(clearInterval);
  timerIntervals = {};
  if (containerRefreshInterval) {
    clearInterval(containerRefreshInterval);
    containerRefreshInterval = null;
  }
  showAuth();
  showToast('Logout realizado!', 'success');
}

// ==========================================
// CONTAINERS LOGIC
// ==========================================

async function loadContainers() {
  try {
    const res = await fetch(`${API}/vm/my`);
    const data = await res.json();
    containers = Array.isArray(data) ? data : [];
    renderContainers();
    updateStats();
  } catch (err) {
    console.error('Erro ao carregar containers:', err);
    containers = [];
    renderContainers();
  }
}

function renderContainers() {
  // Clear old timers
  Object.values(timerIntervals).forEach(clearInterval);
  timerIntervals = {};

  containersGrid.innerHTML = '';

  if (containers.length === 0) {
    noContainers.classList.remove('hidden');
    return;
  }

  noContainers.classList.add('hidden');

  const isPremium = currentUser && currentUser.is_premium;

  containers.forEach(ct => {
    const card = document.createElement('div');
    card.className = 'container-card';
    card.onclick = () => openModal(ct);

    card.innerHTML = `
      <div class="container-card-header">
        <span class="container-name">${ct.name}</span>
        <div class="container-status ${ct.state}">
          <span class="status-dot"></span>
          ${getStatusText(ct.state)}
        </div>
      </div>
      <div class="container-details">
        <div class="container-detail">
          <span class="container-detail-label">IP</span>
          <span class="container-detail-value">${ct.ip || 'Aguardando...'}</span>
        </div>
        <div class="container-detail">
          <span class="container-detail-label">Specs</span>
          <span class="container-detail-value">${ct.cores || 2} vCPUs • ${Math.round((ct.memory || 2048) / 1024)}GB RAM</span>
        </div>
        
        ${getStatsHtml(ct)}

        ${isPremium ? '' : `
        <div class="container-timer" id="timer-${ct.id}">
          ${formatTime(ct.time_remaining)}
        </div>
        `}
      </div>
    `;

    containersGrid.appendChild(card);
    if (!isPremium) {
      startCardTimer(ct);
    }
  });
}

function getStatsHtml(ct) {
  if (ct.state !== 'running') return '';

  // CPU: backend sends float (e.g. 5.5 for 5.5%)
  const cpu = ct.cpu_usage ? ct.cpu_usage.toFixed(1) : '0.0';

  // RAM: backend sends Bytes (memory_usage) and MB (memory)
  // Calc percent
  let ramPercent = 0;
  let ramText = '0 MB';

  if (ct.memory_usage && ct.memory) {
    const totalBytes = ct.memory * 1024 * 1024;
    ramPercent = (ct.memory_usage / totalBytes) * 100;
    const usedMB = ct.memory_usage / 1024 / 1024;

    if (usedMB >= 1024) {
      ramText = `${(usedMB / 1024).toFixed(1)} GB`;
    } else {
      ramText = `${usedMB.toFixed(0)} MB`;
    }
  }

  return `
        <div class="container-stats">
            <div class="stat-row">
                <span class="stat-label">CPU</span>
                <div class="stat-bar-bg">
                    <div class="stat-bar-fill" style="width: ${Math.min(cpu, 100)}%"></div>
                </div>
                <span class="stat-value">${cpu}%</span>
            </div>
            <div class="stat-row">
                <span class="stat-label">RAM</span>
                <div class="stat-bar-bg">
                    <div class="stat-bar-fill" style="width: ${Math.min(ramPercent, 100)}%"></div>
                </div>
                <span class="stat-value">${ramText}</span>
            </div>
        </div>
    `;
}

function startCardTimer(ct) {
  if (timerIntervals[ct.id]) clearInterval(timerIntervals[ct.id]);

  let remaining = ct.time_remaining;
  const timerEl = document.getElementById(`timer-${ct.id}`);

  timerIntervals[ct.id] = setInterval(() => {
    remaining--;
    if (remaining <= 0) {
      clearInterval(timerIntervals[ct.id]);
      loadContainers();
      return;
    }
    if (timerEl) timerEl.textContent = formatTime(remaining);
  }, 1000);
}

function updateStats() {
  const countEl = document.getElementById('container-count');
  if (countEl) countEl.textContent = containers.length;

  const btn = document.getElementById('btn-new-container');
  if (btn) btn.disabled = containers.length >= 3;
}

function getStatusText(state) {
  const texts = {
    running: 'Rodando',
    creating: 'Criando...',
    starting: 'Iniciando...',
    error: 'Erro'
  };
  return texts[state] || state;
}

function formatTime(seconds) {
  if (!seconds || seconds < 0) seconds = 0;
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

// ==========================================
// CONTAINER ACTIONS
// ==========================================

async function createContainer() {
  // Logic moved to handleCreateContainer
}

async function resetTimer(ctid) {
  if (!ctid) return;

  try {
    const res = await fetch(`${API}/vm/${ctid}/reset`, { method: 'POST' });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'Erro ao resetar timer');

    showToast('Timer resetado! +2 horas adicionadas', 'success');
    if (selectedContainer) {
      selectedContainer.time_remaining = data.time_remaining;
      updateModalTimer();

      // Auto-start if stopped
      if (selectedContainer.state !== 'running') {
        startContainer();
      }
    }
    loadContainers();
  } catch (err) {
    showToast(err.message, 'error');
  }
}

async function deleteContainer(ctid) {
  if (!ctid) return;
  if (!confirm('Tem certeza que deseja destruir este container? Esta ação não pode ser desfeita.')) return;

  try {
    const res = await fetch(`${API}/vm/${ctid}`, { method: 'DELETE' });
    if (!res.ok) {
      const data = await res.json();
      throw new Error(data.error || 'Erro ao deletar container');
    }

    showToast('Container destruído com sucesso', 'success');
    closeModal();
    loadContainers();
  } catch (err) {
    showToast(err.message, 'error');
  }
}

// Power Management
async function startContainer() {
  if (!selectedContainer) return;
  showToast('Iniciando container...', 'info');
  try {
    const res = await fetch(`${API}/vm/${selectedContainer.id}/start`, { method: 'POST' });
    if (!res.ok) throw new Error('Erro ao iniciar');

    showToast('Container iniciado', 'success');

    // Optimistic Update
    selectedContainer.state = 'running';
    updatePowerButtons();
    updateModalTimer(); // Start/show timer 

    loadContainers(); // Refresh in background
  } catch (err) {
    showToast(err.message, 'error');
  }
}

async function stopContainer() {
  if (!selectedContainer) return;
  if (!confirm('Deseja parar o container?')) return;
  showToast('Parando container...', 'info');
  try {
    const res = await fetch(`${API}/vm/${selectedContainer.id}/stop`, { method: 'POST' });
    if (!res.ok) throw new Error('Erro ao parar');

    showToast('Container parado', 'success');

    // Optimistic Update
    selectedContainer.state = 'stopped';
    updatePowerButtons();

    loadContainers();
  } catch (err) {
    showToast(err.message, 'error');
  }
}

async function restartContainer() {
  if (!selectedContainer) return;
  if (!confirm('Deseja reiniciar o container?')) return;
  showToast('Reiniciando...', 'info');
  try {
    const res = await fetch(`${API}/vm/${selectedContainer.id}/restart`, { method: 'POST' });
    if (!res.ok) throw new Error('Erro ao reiniciar');
    showToast('Container reiniciado', 'success');

    // Optimistic Update
    // user expecting running state
    selectedContainer.state = 'running';
    updatePowerButtons();

    loadContainers();
  } catch (err) {
    showToast(err.message, 'error');
  }
}

function updatePowerButtons() {
  const btnStart = document.getElementById('modal-btn-start');
  const btnStop = document.getElementById('modal-btn-stop');
  const btnRestart = document.getElementById('modal-btn-restart');
  const btnReset = document.getElementById('modal-btn-reset-big');

  if (!selectedContainer) return;

  const isRunning = selectedContainer.state === 'running';

  // Reset Timer logic: Show for Free users if running or stopped (to renew expiration)
  // But wait, if stopped, reset timer AUTO-STARTS it now.
  // Premium users don't need reset timer usually if infinite.

  if (currentUser && currentUser.is_premium) {
    btnReset.style.display = 'none';
    // Premium: Show power controls based on state
    if (isRunning) {
      btnStart.style.display = 'none';
      btnStop.style.display = 'flex';
      btnRestart.style.display = 'flex';
    } else {
      btnStart.style.display = 'flex';
      btnStop.style.display = 'none';
      btnRestart.style.display = 'none';
    }
  } else {
    // Free user: Always show Reset (+2h)
    btnReset.style.display = 'flex';

    if (isRunning) {
      btnStart.style.display = 'none';
      btnStop.style.display = 'flex'; // Allow stop
      btnRestart.style.display = 'flex'; // Allow restart
    } else {
      // If stopped, use Reset (+2h) to start
      btnStart.style.display = 'none';
      btnStop.style.display = 'none';
      btnRestart.style.display = 'none';
    }
  }
}

// ==========================================
// NEW CONTAINER MODAL (PREMIUM)
// ==========================================

var newContainerModal, cpuSlider, ramSlider, cpuDisplay, ramDisplay, cpuAvail, ramAvail, templateOptionsEl;
var templateCatalog = [];
var selectedTemplate = 'ubuntu';

document.addEventListener('DOMContentLoaded', () => {
  newContainerModal = document.getElementById('new-container-modal');
  cpuSlider = document.getElementById('cpu-slider');
  ramSlider = document.getElementById('ram-slider');
  cpuDisplay = document.getElementById('cpu-display');
  ramDisplay = document.getElementById('ram-display');
  cpuAvail = document.getElementById('cpu-available');
  ramAvail = document.getElementById('ram-available');
  templateOptionsEl = document.getElementById('template-options');
});

function getTemplateIcon(templateId) {
  if (templateId === 'debian') {
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <path d="M12 2L4 6v12l8 4 8-4V6l-8-4z" />
      <path d="M12 22V12" />
      <path d="M20 6l-8 6-8-6" />
    </svg>`;
  }
  return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
    <path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5" />
  </svg>`;
}

function renderTemplateOptions() {
  if (!templateOptionsEl) return;
  if (!templateCatalog.length) {
    templateOptionsEl.innerHTML = '<div class="empty-state">Nenhum template disponível.</div>';
    return;
  }

  if (!templateCatalog.find(tmpl => tmpl.id === selectedTemplate)) {
    selectedTemplate = templateCatalog[0].id;
  }

  templateOptionsEl.innerHTML = templateCatalog.map(tmpl => {
    const isSelected = tmpl.id === selectedTemplate;
    const desc = tmpl.description ? tmpl.description : 'Template disponível';
    return `
      <div class="template-selector ${isSelected ? 'selected' : ''}" data-template="${tmpl.id}">
        <div class="template-icon">
          ${getTemplateIcon(tmpl.id)}
        </div>
        <div class="template-info">
          <span class="template-name">${tmpl.name || tmpl.id}</span>
          <span class="template-desc">${desc}</span>
        </div>
        <div class="template-check">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3">
            <polyline points="20 6 9 17 4 12" />
          </svg>
        </div>
      </div>
    `;
  }).join('');

  templateOptionsEl.querySelectorAll('.template-selector').forEach(el => {
    el.addEventListener('click', () => {
      selectedTemplate = el.dataset.template;
      renderTemplateOptions();
    });
  });
}

async function loadTemplates() {
  try {
    const res = await fetch(`${API}/vm/templates`);
    if (!res.ok) return;
    const data = await res.json();
    templateCatalog = Array.isArray(data) ? data : [];
    renderTemplateOptions();
  } catch (err) {
    console.error(err);
  }
}

function setupSliders() {
  [cpuSlider, ramSlider].forEach(slider => {
    slider.addEventListener('input', updateSliderVisuals);
  });
  // Init state
  updateSliderVisuals.call(cpuSlider);
  updateSliderVisuals.call(ramSlider);
}

function updateSliderVisuals() {
  const val = parseInt(this.value);
  const min = parseInt(this.min);
  const max = parseInt(this.max);

  // Update labels
  if (this.id === 'cpu-slider') {
    document.getElementById('cpu-display').textContent = `${val} vCPUs`;
  } else {
    document.getElementById('ram-display').textContent = `${(val / 1024).toFixed(0)} GB`;
  }

  // Update Track Fill
  const percent = ((val - min) / (max - min)) * 100;
  const trackId = this.id === 'cpu-slider' ? 'cpu-track' : 'ram-track';
  document.getElementById(trackId).style.width = `${percent}%`;
}

async function showNewContainerModal() {
  const maxContainers = currentUser?.max_containers ?? 3;
  if (containers.length >= maxContainers) {
    showToast(`Limite de containers atingido (máx: ${maxContainers})`, 'error');
    return;
  }

  // Check resources
  await updateResourceLimitsDisplay();

  if (!templateCatalog.length) {
    await loadTemplates();
  } else {
    renderTemplateOptions();
  }

  // Reset to min defaults or smart defaults
  cpuSlider.value = 2;
  ramSlider.value = 2048;
  updateSliderVisuals.call(cpuSlider);
  updateSliderVisuals.call(ramSlider);

  newContainerModal.classList.add('active');
}

async function updateResourceLimitsDisplay() {
  try {
    const res = await fetch(`${API}/account/resources`);
    if (!res.ok) return;
    const data = await res.json();

    cpuAvail.textContent = data.available_cores;
    const availableMemory = Math.max(0, data.available_memory || 0);
    ramAvail.textContent = (availableMemory / 1024).toFixed(1);

    // Update Max Limits Labels and Inputs
    const cpuMaxLabel = document.getElementById('cpu-max-label');
    const ramMaxLabel = document.getElementById('ram-max-label');

    if (cpuMaxLabel) cpuMaxLabel.textContent = `Max: ${data.max_cores_per_container}`;
    if (ramMaxLabel) ramMaxLabel.textContent = `Max: ${data.max_memory_per_container / 1024} GB`;

    // Update Slider Max Attributes
    cpuSlider.max = data.max_cores_per_container;
    ramSlider.max = data.max_memory_per_container;

    // Refresh visuals if current value is out of bounds (though backend validates)
    // or just to update the slider track fill calculation
    updateSliderVisuals.call(cpuSlider);
    updateSliderVisuals.call(ramSlider);

  } catch (err) {
    console.error(err);
  }
}

function closeNewContainerModal() {
  newContainerModal.classList.remove('active');
}

async function handleCreateContainer(e) {
  e.preventDefault();

  const btn = document.getElementById('btn-create-submit');
  const originalContent = btn.innerHTML;
  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span> Criando...';

  const cores = parseInt(cpuSlider.value);
  const memory = parseInt(ramSlider.value);

  try {
    const res = await fetch(`${API}/vm/create`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        template: selectedTemplate,
        cores: cores,
        memory: memory
      })
    });

    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'Erro ao criar container');

    showToast('Container criado com sucesso!', 'success');
    closeNewContainerModal();
    // Switch to containers view if not there
    document.querySelector('[data-view="containers"]').click();
    await loadContainers();
  } catch (err) {
    showToast(err.message, 'error');
  } finally {
    btn.disabled = false;
    btn.innerHTML = originalContent;
  }
}

// ==========================================
// FIREWALL LOGIC
// ==========================================

function setupFirewall() {
  document.getElementById('btn-add-rule').addEventListener('click', openFirewallModal);
  document.getElementById('firewall-form').addEventListener('submit', handleCreateFirewallRule);
}

// Called when switching to firewall view
async function loadFirewallView() {
  // Refresh containers list to ensure we have IDs
  await loadContainers(); // Re-fetch to be safe

  // In future we might have a container selector in the view header to filter table
  // For now, let's load ALL rules by iterating containers (inefficient but works for 3 containers)
  loadAllFirewallRules();
}

async function loadAllFirewallRules() {
  firewallLoading.classList.remove('hidden');
  firewallTableBody.innerHTML = '';
  firewallEmpty.classList.add('hidden');

  let allRules = [];

  // Fetch rules for each container
  for (const ct of containers) {
    try {
      const res = await fetch(`${API}/firewall?container_id=${ct.id}`);
      if (res.ok) {
        const rules = await res.json();
        // Add container name to rule for display
        rules.forEach(r => r.containerName = ct.name);
        allRules = [...allRules, ...rules];
      }
    } catch (e) {
      console.error(e);
    }
  }

  firewallLoading.classList.add('hidden');
  if (allRules.length === 0) {
    firewallEmpty.classList.remove('hidden');
  } else {
    renderFirewallRules(allRules);
  }
}

function renderFirewallRules(rules) {
  firewallTableBody.innerHTML = '';
  rules.forEach(rule => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
            <td>${rule.id}</td>
            <td>${rule.containerName || rule.container_id}</td>
            <td><span class="badge badge-${rule.protocol}">${rule.protocol.toUpperCase()}</span></td>
            <td>${rule.internal_port}</td>
            <td class="mono text-accent">${rule.external_port}</td>
            <td>
                <button class="btn-sm-danger" onclick="deleteFirewallRule(${rule.id})">
                    <i class="fas fa-trash"></i> Excluir
                </button>
            </td>
        `;
    firewallTableBody.appendChild(tr);
  });
}

function openFirewallModal() {
  // Populate select
  const select = document.getElementById('fw-container-select');
  select.innerHTML = '<option value="">Selecione...</option>';

  containers.forEach(ct => {
    const opt = document.createElement('option');
    opt.value = ct.id;
    opt.textContent = `${ct.name} (${ct.ip})`;
    select.appendChild(opt);
  });

  firewallModal.classList.remove('hidden');
  firewallModal.classList.add('active'); // Use active class for animation
}

function closeFirewallModal() {
  firewallModal.classList.remove('active');
  setTimeout(() => firewallModal.classList.add('hidden'), 300); // Wait for anim
}

async function handleCreateFirewallRule(e) {
  e.preventDefault();
  const btn = e.target.querySelector('button[type="submit"]');
  const oldText = btn.innerHTML;
  btn.disabled = true;
  btn.textContent = 'Criando...';

  const containerId = parseInt(document.getElementById('fw-container-select').value);
  const protocol = document.querySelector('input[name="protocol"]:checked').value;
  const port = parseInt(document.getElementById('fw-internal-port').value);

  try {
    const res = await fetch(`${API}/firewall/create`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        container_id: containerId,
        internal_port: port,
        protocol: protocol
      })
    });

    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'Erro ao criar regra');

    showToast('Regra criada com sucesso!', 'success');
    closeFirewallModal();
    loadAllFirewallRules();
  } catch (err) {
    showToast(err.message, 'error');
  } finally {
    btn.disabled = false;
    btn.innerHTML = oldText;
  }
}

window.deleteFirewallRule = async function (id) {
  if (!confirm('Deseja realmente remover esta regra de firewall?')) return;

  try {
    const res = await fetch(`${API}/firewall/${id}`, { method: 'DELETE' });
    if (!res.ok) throw new Error('Erro ao deletar regra');

    showToast('Regra removida!', 'success');
    loadAllFirewallRules();
  } catch (err) {
    showToast(err.message, 'error');
  }
}

// ==========================================
// MODAL & UTILS (Existing)
// ==========================================

function setupModalTabs() {
  const tabs = document.querySelectorAll('.modal-tab');
  tabs.forEach(tab => {
    tab.addEventListener('click', () => {
      console.log('Tab clicked:', tab.dataset.tab); // DEBUG
      // Remove active from all tabs
      document.querySelectorAll('.modal-tab').forEach(t => t.classList.remove('active'));
      document.querySelectorAll('.modal-tab-content').forEach(c => c.classList.remove('active'));

      // Activate clicked
      tab.classList.add('active');
      const targetId = `tab-${tab.dataset.tab}`;
      const target = document.getElementById(targetId);
      if (target) target.classList.add('active');

      // Refresh terminal if visible
      if (tab.dataset.tab === 'terminal' && terminal) {
        setTimeout(() => {
          if (fitAddon) fitAddon.fit();
          terminal.focus();
        }, 200);
      }

      if (tab.dataset.tab === 'snapshots') {
        loadSnapshots(selectedContainer?.id);
      }
      if (tab.dataset.tab === 'monitoring') {
        loadMonitoring(selectedContainer?.id);
      }
      if (tab.dataset.tab === 'files') {
        loadFiles(selectedContainer?.id, currentPath);
      }
    });
  });
}

function openModal(ct) {
  selectedContainer = ct;
  currentPath = "/";
  containerModal.classList.add('active');
  document.getElementById('modal-title').textContent = ct.name;

  document.querySelectorAll('.modal-tab').forEach(t => t.classList.remove('active'));
  document.querySelectorAll('.modal-tab-content').forEach(c => c.classList.remove('active'));
  document.querySelector('.modal-tab[data-tab="info"]')?.classList.add('active');
  document.getElementById('tab-info')?.classList.add('active');

  const statusEl = document.getElementById('modal-status');
  statusEl.className = `modal-status ${ct.state}`;
  document.getElementById('modal-status-text').textContent = getStatusText(ct.state);

  document.getElementById('modal-ip').textContent = ct.ip || 'Aguardando...';
  document.getElementById('modal-password').textContent = ct.password || '••••••';

  // PREMIUM CHECKS
  const isPremium = currentUser && currentUser.is_premium;

  // Timer Logic
  const timerCard = document.querySelector('.info-card .timer-icon')?.parentNode;
  const resetBtnSmall = document.getElementById('modal-btn-reset');
  const resetBtnBig = document.getElementById('modal-btn-reset-big');
  const timerValue = document.getElementById('modal-timer');

  if (isPremium) {
    if (timerCard) timerCard.style.display = 'none';
    if (timerValue) timerValue.textContent = "";
    if (resetBtnSmall) resetBtnSmall.style.display = 'none';
    if (resetBtnBig) resetBtnBig.style.display = 'none';
  } else {
    if (timerCard) timerCard.style.display = 'flex';
    if (resetBtnSmall) resetBtnSmall.style.display = 'flex';
    if (resetBtnBig) resetBtnBig.style.display = 'flex';
    updateModalTimer();
  }

  updateExternalSSH(ct);
  if (!isPremium) updateModalTimer(); // Only start timer loop if not premium? 
  // Actually updateModalTimer handles the loop. We should kill it if premium.
  if (isPremium && modalTimerInterval) clearInterval(modalTimerInterval);

  initTerminal(ct.id);
  startModalPolling();
}

function setupMonitoring() {
  const timeframeSelect = document.getElementById('monitoring-timeframe');
  if (!timeframeSelect) return;
  timeframeSelect.addEventListener('change', () => {
    loadMonitoring(selectedContainer?.id);
  });
}

// ==========================================
// SNAPSHOTS LOGIC
// ==========================================

async function loadSnapshots(ctid) {
  const tbody = document.getElementById('snapshots-tbody');
  const loading = document.getElementById('snapshots-loading');
  const empty = document.getElementById('snapshots-empty');
  if (!ctid || !tbody || !loading || !empty) return;

  loading.classList.remove('hidden');
  empty.classList.add('hidden');
  tbody.innerHTML = '';

  try {
    const res = await fetch(`${API}/vm/${ctid}/snapshots`);
    if (!res.ok) throw new Error('Erro ao carregar snapshots');

    const data = await res.json();
    const snapshots = Array.isArray(data) ? data : [];
    loading.classList.add('hidden');

    if (!snapshots.length) {
      empty.classList.remove('hidden');
      return;
    }

    snapshots.forEach(snapshot => {
      const safeName = JSON.stringify(snapshot.name || '');
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td>${snapshot.name || snapshot.snaptime || 'snapshot'}</td>
        <td>${snapshot.snaptime ? new Date(snapshot.snaptime * 1000).toLocaleString() : '-'}</td>
        <td>
          <button class="btn-sm" onclick="restoreSnapshot(${safeName})"><i class="fas fa-rotate-left"></i></button>
          <button class="btn-sm-danger" onclick="deleteSnapshot(${safeName})"><i class="fas fa-trash"></i></button>
        </td>
      `;
      tbody.appendChild(tr);
    });
  } catch (err) {
    loading.classList.add('hidden');
    empty.classList.remove('hidden');
    showToast(err.message, 'error');
  }
}

window.createSnapshot = async function () {
  if (!selectedContainer) return;
  const name = prompt('Nome do snapshot:');
  if (!name) return;

  try {
    const res = await fetch(`${API}/vm/${selectedContainer.id}/snapshots`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    });
    if (!res.ok) {
      const data = await res.json();
      throw new Error(data.error || 'Erro ao criar snapshot');
    }

    showToast('Snapshot criado com sucesso!', 'success');
    loadSnapshots(selectedContainer.id);
  } catch (err) {
    showToast(err.message, 'error');
  }
};

window.restoreSnapshot = async function (name) {
  if (!selectedContainer || !name) return;
  if (!confirm(`Deseja restaurar o snapshot "${name}"?`)) return;

  try {
    const res = await fetch(`${API}/vm/${selectedContainer.id}/snapshots/${encodeURIComponent(name)}/restore`, {
      method: 'POST'
    });
    if (!res.ok) {
      const data = await res.json();
      throw new Error(data.error || 'Erro ao restaurar snapshot');
    }
    showToast('Snapshot restaurado com sucesso!', 'success');
  } catch (err) {
    showToast(err.message, 'error');
  }
};

window.deleteSnapshot = async function (name) {
  if (!selectedContainer || !name) return;
  if (!confirm(`Deseja deletar o snapshot "${name}"?`)) return;

  try {
    const res = await fetch(`${API}/vm/${selectedContainer.id}/snapshots/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    });
    if (!res.ok) {
      const data = await res.json();
      throw new Error(data.error || 'Erro ao deletar snapshot');
    }
    showToast('Snapshot removido!', 'success');
    loadSnapshots(selectedContainer.id);
  } catch (err) {
    showToast(err.message, 'error');
  }
};

// ==========================================
// MONITORING LOGIC
// ==========================================

async function loadMonitoring(ctid) {
  const status = document.getElementById('monitoring-status');
  const cpuCurrent = document.getElementById('monitoring-cpu-current');
  const ramCurrent = document.getElementById('monitoring-ram-current');
  const timeframeSelect = document.getElementById('monitoring-timeframe');
  if (!ctid || !status || !cpuCurrent || !ramCurrent || !timeframeSelect) return;

  status.textContent = 'Carregando...';
  const timeframe = timeframeSelect.value || 'hour';

  try {
    const res = await fetch(`${API}/vm/${ctid}/graphs?timeframe=${encodeURIComponent(timeframe)}`);
    if (!res.ok) throw new Error('Erro ao carregar métricas');

    const points = await res.json();
    status.textContent = points.length ? 'Atualizado' : 'Sem dados';

    const labels = points.map(point => new Date(point.time * 1000).toLocaleTimeString());
    const cpuSeries = points.map(point => (point.cpu || 0) * 100);
    const memSeries = points.map(point => {
      if (!point.mem || !point.maxmem) return 0;
      return (point.mem / point.maxmem) * 100;
    });

    const latestCpu = cpuSeries[cpuSeries.length - 1] || 0;
    const latestMem = memSeries[memSeries.length - 1] || 0;
    cpuCurrent.textContent = `${latestCpu.toFixed(1)}%`;
    ramCurrent.textContent = `${latestMem.toFixed(1)}%`;

    updateChart('cpu', labels, cpuSeries, 'CPU (%)', 'rgba(255, 77, 0, 0.6)');
    updateChart('memory', labels, memSeries, 'Memória (%)', 'rgba(59, 130, 246, 0.6)');
  } catch (err) {
    status.textContent = 'Erro';
    showToast(err.message, 'error');
  }
}

function updateChart(type, labels, data, label, color) {
  const canvasId = type === 'cpu' ? 'cpu-chart' : 'memory-chart';
  const ctx = document.getElementById(canvasId);
  if (!ctx) return;

  const chartRef = type === 'cpu' ? cpuChart : memChart;
  if (chartRef) chartRef.destroy();

  const chart = new Chart(ctx, {
    type: 'line',
    data: {
      labels,
      datasets: [{
        label,
        data,
        borderColor: color,
        backgroundColor: color.replace('0.6', '0.15'),
        fill: true,
        tension: 0.25,
        pointRadius: 0
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: {
          display: false
        }
      },
      scales: {
        y: {
          beginAtZero: true,
          max: 100,
          ticks: { color: '#9ca3af' },
          grid: { color: 'rgba(255,255,255,0.05)' }
        },
        x: {
          ticks: { color: '#9ca3af', maxTicksLimit: 6 },
          grid: { color: 'rgba(255,255,255,0.05)' }
        }
      }
    }
  });

  if (type === 'cpu') {
    cpuChart = chart;
  } else {
    memChart = chart;
  }
}

function updateExternalSSH(ct) {
  const card = document.getElementById('card-external-ssh');
  const valueEl = document.getElementById('modal-external-ssh');

  // Logic: Check if there's an SSH port forward?
  // Actually, the new Firewall system allows ANY port forward.
  // But usually we had automatic SSH. 
  // If `ct.external_port` exists (legacy field from DB?) 
  // Wait, I didn't verify if `containers` table still has `external_port`.
  // My new system uses `port_forwards` table.
  // The 'Automatic SSH' logic in backend `createContainer` still exists?
  // Yes, I didn't remove `mikrotik.CreatePortForwarding` from `CreateContainer` in backend?
  // Let's assume the backend 'CreateContainer' STILL does the default SSH mapping and sets `external_port` in `containers` table.
  // If I haven't changed that, it should still work.
  // Checking `vm.go`: `CreateContainer` -> `m.runSSHConfig` -> `m.mikrotik.GetFreePort` -> DB Save.
  // So yes, `external_port` on `containers` table is likely still valid for the MAIN SSH.
  // The firewall module is for ADDITIONAL ports.

  if (ct.external_port && ct.external_port > 0 && ct.public_ip) {
    card.style.display = 'flex';
    valueEl.textContent = `ssh root@${ct.public_ip} -p ${ct.external_port}`;
  } else {
    card.style.display = 'none';
  }
}

function closeModal() {
  containerModal.classList.remove('active');
  if (terminalSocket) terminalSocket.close();
  if (terminal) terminal.dispose();
  terminal = null;
  terminalSocket = null;
  selectedContainer = null;

  if (modalTimerInterval) clearInterval(modalTimerInterval);
  if (modalPollInterval) clearInterval(modalPollInterval);
}

let modalTimerInterval = null;
function updateModalTimer() {
  // ... existing timer logic ...
  if (modalTimerInterval) clearInterval(modalTimerInterval);
  if (!selectedContainer) return;

  let remaining = selectedContainer.time_remaining;
  const timerEl = document.getElementById('modal-timer');
  if (timerEl) timerEl.textContent = formatTime(remaining);

  modalTimerInterval = setInterval(() => {
    remaining--;
    if (remaining <= 0) {
      clearInterval(modalTimerInterval);
      return;
    }
    if (timerEl) timerEl.textContent = formatTime(remaining);
  }, 1000);
}

let modalPollInterval = null;
function startModalPolling() {
  if (modalPollInterval) clearInterval(modalPollInterval);

  modalPollInterval = setInterval(async () => {
    if (!selectedContainer) return;
    try {
      const res = await fetch(`${API}/vm/${selectedContainer.id}`);
      if (res.ok) {
        const data = await res.json();
        // Update helpers...
        if (data.ip) document.getElementById('modal-ip').textContent = data.ip;
        document.getElementById('modal-password').textContent = data.password || '••••••';
        updateExternalSSH(data);
        updatePowerButtons(); // Update buttons based on new state
        // ...
      }
    } catch (e) { }
  }, 5000);
}

// Terminal & Utils
// Terminal & Utils
function initTerminal(vmId) {
  if (terminal) {
    terminal.dispose();
  }

  terminal = new Terminal({
    cursorBlink: true,
    fontSize: 14,
    fontFamily: 'Menlo, Monaco, "Courier New", monospace',
    theme: {
      background: '#0d1117',
      foreground: '#c9d1d9',
    }
  });

  const fitAddon = new FitAddon.FitAddon();
  terminal.loadAddon(fitAddon);

  terminal.open(document.getElementById('terminal-container'));
  fitAddon.fit();

  terminal.onData(data => {
    if (terminalSocket && terminalSocket.readyState === WebSocket.OPEN) {
      terminalSocket.send(data);
    }
  });

  // Handle resize with ResizeObserver for better responsiveness
  const resizeObserver = new ResizeObserver(() => {
    try {
      fitAddon.fit();
      // Send resize command to backend
      if (terminalSocket && terminalSocket.readyState === WebSocket.OPEN) {
        // fitAddon.proposeDimensions() is not always accurate if called immediately
        // But terminal.cols and terminal.rows are updated after fit()
        const dims = { cols: terminal.cols, rows: terminal.rows };
        terminalSocket.send(JSON.stringify({ type: 'resize', ...dims }));
      }
    } catch (e) { }
  });
  resizeObserver.observe(document.getElementById('terminal-container'));

  // Connect WS
  connectTerminalWS(vmId);

  // Initial fit
  setTimeout(() => {
    try {
      fitAddon.fit();
      if (terminalSocket && terminalSocket.readyState === WebSocket.OPEN) {
        const dims = { cols: terminal.cols, rows: terminal.rows };
        terminalSocket.send(JSON.stringify({ type: 'resize', ...dims }));
      }
    } catch (e) { }
    terminal.focus();
  }, 300);
}
// (Include the rest of Terminal functions: connectTerminalWS, sendTerminalSize, etc.)
// ...

function connectTerminalWS(vmId) {
  // ... standard websocket logic ...
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${protocol}//${location.host}/api/vm/${vmId}/terminal`;
  if (terminalSocket) terminalSocket.close();
  terminalSocket = new WebSocket(wsUrl, 'binary');
  terminalSocket.binaryType = 'arraybuffer';
  // ... setup handlers ...
  terminalSocket.onmessage = (e) => {
    if (terminal) {
      if (e.data instanceof ArrayBuffer) {
        terminal.write(new Uint8Array(e.data));
      } else {
        terminal.write(e.data);
      }
    }
  };
  terminalSocket.onopen = () => terminal?.writeln('\r\n\x1b[32mConectado via WebSocket!\x1b[0m\r\n');
}

// ... CopyText, ShowToast ...
function copyText(elementId) {
  const el = document.getElementById(elementId);
  if (!el) return;
  navigator.clipboard.writeText(el.textContent).then(() => showToast('Copiado!', 'success'));
}

function showToast(msg, type = 'info') {
  const box = document.createElement('div');
  box.className = `toast ${type}`;
  box.textContent = msg;
  document.getElementById('toast-container').appendChild(box);
  setTimeout(() => box.remove(), 3000);
}

// Keyboard
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    closeModal();
    closeNewContainerModal();
    closeFirewallModal();
  }
});

function getStatusText(state) {
  switch (state) {
    case 'running': return 'Rodando';
    case 'stopped': return 'Parado';
    case 'creating': return 'Criando...';
    case 'provisioning': return 'Configurando Rede...';
    case 'error': return 'Erro';
    default: return state;
  }
}

// ==========================================
// ADMIN PANEL LOGIC
// ==========================================

async function loadAdminView() {
  await Promise.all([
    fetchAdminStats(),
    fetchAdminUsers(),
    fetchAdminContainers()
  ]);
}

async function fetchAdminStats() {
  try {
    const res = await fetch(`${API}/admin/stats`);
    if (res.ok) {
      const stats = await res.json();
      const node = stats.node || stats;
      document.getElementById('admin-cpu').textContent = (node.cpu * 100).toFixed(1) + '%';

      const totalRam = (node.memory.total / 1024 / 1024 / 1024).toFixed(1);
      const usedRam = (node.memory.used / 1024 / 1024 / 1024).toFixed(1);
      document.getElementById('admin-ram').textContent = `${usedRam}/${totalRam} GB`;

      const uptimeDays = Math.floor(node.uptime / 86400);
      document.getElementById('admin-uptime').textContent = `${uptimeDays} dias`;

      const totalUsers = document.getElementById('admin-users-count');
      const totalContainers = document.getElementById('admin-containers-count');
      const runningContainers = document.getElementById('admin-running-count');

      if (totalUsers) totalUsers.textContent = stats.total_users ?? '-';
      if (totalContainers) totalContainers.textContent = stats.total_containers ?? '-';
      if (runningContainers) runningContainers.textContent = stats.running_containers ?? '-';
    }
  } catch (e) { console.error(e); }
}

async function fetchAdminUsers() {
  try {
    const res = await fetch(`${API}/admin/users`);
    if (res.ok) {
      const users = await res.json();
      console.log('DEBUG: Admin Users received:', users);
      const tbody = document.querySelector('#admin-users-table tbody');
      tbody.innerHTML = '';
      users.forEach(u => {
        const tr = document.createElement('tr');
        const registerIP = u.register_ip || '-';
        const safeUsername = u.username.replace(/'/g, "\\'"); // Escape single quotes for onclick
        tr.innerHTML = `
          <td>${u.id}</td>
          <td>${u.username} ${u.is_premium ? '<span class="badge premium">PREMIUM</span>' : ''} ${u.is_admin ? '<span class="badge admin">ADMIN</span>' : ''}</td>
          <td>${u.container_count || 0}</td>
          <td>${registerIP}</td>
          <td>${u.last_login || '-'}</td>
          <td style="display: flex; gap: 0.5rem; justify-content: flex-end;">
            <button class="btn-sm" onclick="toggleUserPremium(${u.id}, ${!u.is_premium})" title="${u.is_premium ? 'Remover Premium' : 'Dar Premium'}">
              <i class="fas fa-crown" style="color: ${u.is_premium ? 'var(--text-muted)' : 'gold'}"></i>
            </button>
            <button class="btn-sm" onclick="toggleUserBan(${u.id}, ${!u.is_banned}, '${safeUsername}')" title="${u.is_banned ? 'Desbanir' : 'Banir'}">
              <i class="fas fa-gavel" style="color: ${u.is_banned ? 'var(--success)' : 'var(--error)'}"></i>
            </button>
            <button class="btn-sm" onclick="deleteUser(${u.id}, '${safeUsername}')" title="Deletar Usuário">
              <i class="fas fa-trash" style="color: var(--error)"></i>
            </button>
          </td>
        `;
        tbody.appendChild(tr);
      });
    }
  } catch (err) {
    showToast('Erro ao carregar usuários: ' + err.message, 'error');
  }
}

async function fetchAdminContainers() {
  try {
    const res = await fetch(`${API}/admin/containers`);
    if (res.ok) {
      const list = await res.json();
      const tbody = document.querySelector('#admin-containers-table tbody');
      tbody.innerHTML = '';
      list.forEach(c => {
        const tr = document.createElement('tr');
        tr.innerHTML = `
          <td>${c.id}</td>
          <td>${c.name}</td>
          <td>${c.owner}</td>
          <td>${c.ip}</td>
          <td><span class="badge badge-${c.state}">${c.state}</span></td>
          <td>${Math.round(c.memory / 1024)}GB / ${c.cores}vCPU</td>
          <td>
            <button class="btn-sm" onclick="openAdminContainer(${c.id})"><i class="fas fa-terminal"></i></button>
            <button class="btn-sm-danger" onclick="adminDeleteContainer(${c.id})"><i class="fas fa-trash"></i></button>
          </td>
        `;
        tbody.appendChild(tr);
      });
    }
  } catch (e) { console.error(e); }
}

window.openAdminContainer = async function (ctid) {
  try {
    const res = await fetch(`${API}/vm/${ctid}`);
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      throw new Error(data.error || 'Erro ao carregar container');
    }
    const container = await res.json();
    openModal(container);
  } catch (err) {
    showToast(err.message, 'error');
  }
};

window.adminDeleteContainer = async function (ctid) {
  if (!confirm(`Deseja deletar o container ${ctid}?`)) return;
  try {
    const res = await fetch(`${API}/vm/${ctid}`, { method: 'DELETE' });
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      throw new Error(data.error || 'Erro ao deletar container');
    }
    showToast('Container deletado com sucesso!', 'success');
    loadAdminView();
  } catch (err) {
    showToast(err.message, 'error');
  }
};

window.handleResetUserPassword = async function (userId, username) {
  const newPass = prompt(`Digite a nova senha para ${username}:`);
  if (!newPass) return;
  if (newPass.length < 6) {
    alert('A senha deve ter no mínimo 6 caracteres');
    return;
  }

  try {
    const res = await fetch(`${API}/admin/reset-password`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ user_id: userId, new_password: newPass })
    });
    if (res.ok) {
      showToast('Senha alterada com sucesso!', 'success');
    } else {
      const d = await res.json();
      showToast(d.error, 'error');
    }
  } catch (e) {
    showToast('Erro ao resetar senha', 'error');
  }
};

window.toggleUserPremium = async function (userId, currentStatus) {
  const action = currentStatus ? 'remover' : 'adicionar';
  if (!confirm(`Deseja realmente ${action} o status Premium deste usuário?`)) return;

  try {
    const res = await fetch(`${API}/admin/users/${userId}/premium`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ premium: !currentStatus })
    });

    if (res.ok) {
      showToast('Status atualizado com sucesso!', 'success');
      loadAdminView(); // Recarrega a lista
    } else {
      const d = await res.json();
      showToast(d.error || 'Erro ao atualizar', 'error');
    }
  } catch (e) {
    showToast('Erro de conexão', 'error');
  }
};

// NODE TERMINAL
let nodeTerminal = null;
let nodeSocket = null;
let nodeFitAddon = null;

window.openNodeTerminal = function () {
  const modal = document.getElementById('node-terminal-modal');
  modal.classList.remove('hidden');
  modal.classList.add('active');
  setTimeout(() => initNodeTerminal(), 300); // Wait for modal anim
};

window.closeNodeTerminalModal = function () {
  const modal = document.getElementById('node-terminal-modal');
  modal.classList.remove('active');
  setTimeout(() => modal.classList.add('hidden'), 300);
  if (nodeSocket) nodeSocket.close();
  if (nodeTerminal) nodeTerminal.dispose();
  nodeTerminal = null;
  nodeSocket = null;
};

function initNodeTerminal() {
  const container = document.getElementById('node-terminal-container');
  container.innerHTML = '';

  if (nodeTerminal) nodeTerminal.dispose();

  nodeTerminal = new Terminal({
    cursorBlink: true,
    fontSize: 14,
    fontFamily: 'Menlo, Monaco, "Courier New", monospace',
    theme: { background: '#0d1117', foreground: '#c9d1d9' }
  });

  nodeFitAddon = new FitAddon.FitAddon();
  nodeTerminal.loadAddon(nodeFitAddon);
  nodeTerminal.open(container);
  nodeFitAddon.fit();

  // Connect
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${protocol}//${location.host}/api/admin/node-terminal`;

  nodeSocket = new WebSocket(wsUrl);
  nodeSocket.binaryType = 'arraybuffer'; // Proxmox sends binary? usually raw bytes

  nodeSocket.onopen = () => {
    nodeTerminal.writeln('\r\n\x1b[32mConectado ao Proxmox Node!\x1b[0m\r\n');
    nodeFitAddon.fit();
  };

  nodeSocket.onmessage = (e) => {
    if (typeof e.data === 'string') {
      nodeTerminal.write(e.data);
    } else {
      nodeTerminal.write(new Uint8Array(e.data));
    }
  };

  nodeSocket.onclose = () => {
    nodeTerminal?.writeln('\r\n\x1b[31mDesconectado.\x1b[0m\r\n');
  };

  nodeTerminal.onData(data => {
    console.log('Terminal input:', data); // Debug
    if (nodeSocket && nodeSocket.readyState === WebSocket.OPEN) {
      nodeSocket.send(data);
    }
  });

  // Resize handling? Proxmox VNC doesn't always support dynamic resize via channel, 
  // or it might expect specific protocol messages. 
  // For now simple pipe.

  window.addEventListener('resize', () => {
    try { nodeFitAddon.fit(); } catch (e) { }
  });
}

window.toggleUserBan = async function (userId, isBanned, currentReason) {
  let ban = !isBanned;
  let reason = "";

  if (ban) {
    reason = prompt("Motivo do banimento:");
    if (!reason) return; // Cancelou
  } else {
    if (!confirm("Tem certeza que deseja desbanir este usuário?")) return;
  }

  try {
    const res = await fetch(`${API}/admin/users/${userId}/ban`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ban: ban, reason: reason })
    });

    if (res.ok) {
      loadAdminView(); // Recarrega a tabela
    } else {
      const err = await res.json();
      alert("Erro: " + err.error);
    }
  } catch (e) {
    console.error(e);
    alert("Erro de conexão");
  }
};

window.deleteUser = async function (uid, username) {
  if (!confirm(`⚠️ PERIGO: Tem certeza que deseja DELETAR o usuário "${username}"?\n\nIsso apagará PERMANENTEMENTE:\n- A conta do usuário\n- TODAS as VMs e dados\n- Todas as regras de firewall\n\nEsta ação não pode ser desfeita!`)) return;

  try {
    const res = await fetch(`${API}/admin/users/${uid}`, {
      method: 'DELETE'
    });

    if (!res.ok) {
      const data = await res.json();
      throw new Error(data.error || 'Erro ao deletar usuário');
    }

    showToast('Usuário deletado com sucesso!', 'success');
    loadAdminView();
  } catch (err) {
    showToast(err.message, 'error');
  }
};

// ==========================================
// FILE MANAGER LOGIC
// ==========================================

async function loadFiles(ctid, path) {
  currentPath = path || "/";
  const tbody = document.getElementById('files-tbody');
  const loading = document.getElementById('files-loading');
  const error = document.getElementById('files-error');
  const backBtn = document.getElementById('files-back-btn');

  // Check if elements exist (safety)
  if (!tbody) { console.error("TBody missing"); return; }

  tbody.innerHTML = '';
  loading.classList.remove('hidden');
  error.classList.add('hidden');

  updateBreadcrumbs(ctid);
  if (backBtn) {
    backBtn.disabled = currentPath === "/";
    backBtn.style.opacity = currentPath === "/" ? "0.5" : "1";
    backBtn.style.cursor = currentPath === "/" ? "not-allowed" : "pointer";
  }

  try {
    const res = await fetch(`${API}/vm/${ctid}/files?path=${encodeURIComponent(currentPath)}`);
    if (res.ok) {
      const data = await res.json();
      const files = Array.isArray(data) ? data : [];
      loading.classList.add('hidden');

      files.sort((a, b) => {
        if (a.is_dir && !b.is_dir) return -1;
        if (!a.is_dir && b.is_dir) return 1;
        return a.name.localeCompare(b.name);
      });

      if (currentPath !== "/") {
        const tr = document.createElement('tr');
        tr.innerHTML = `
                    <td><i class="fas fa-folder" style="color:var(--warning);"></i></td>
                    <td><a href="#" onclick="navigateUp(${ctid}); return false;">..</a></td>
                    <td>-</td>
                    <td>-</td>
                    <td>-</td>
                    <td></td>
                `;
        tbody.appendChild(tr);
      }

      if (!files.length) {
        const tr = document.createElement('tr');
        tr.innerHTML = `
          <td colspan="6" style="text-align:center; color:var(--text-secondary); padding:1rem;">
            Nenhum arquivo encontrado.
          </td>
        `;
        tbody.appendChild(tr);
        return;
      }

      files.forEach(f => {
        const tr = document.createElement('tr');
        const icon = f.is_dir ? 'fa-folder' : 'fa-file-alt';
        const color = f.is_dir ? 'var(--warning)' : 'var(--text-secondary)';
        let size = f.size + " B";
        if (f.size > 1024) size = (f.size / 1024).toFixed(1) + " KB";
        if (f.size > 1024 * 1024) size = (f.size / 1024 / 1024).toFixed(1) + " MB";

        tr.innerHTML = `
                    <td><i class="fas ${icon}" style="color:${color};"></i></td>
                    <td>
                        <a href="#" onclick="${f.is_dir ? `loadFiles(${ctid}, '${joinPath(currentPath, f.name)}')` : `openEditor(${ctid}, '${joinPath(currentPath, f.name)}')`}; return false;">
                            ${f.name}
                        </a>
                    </td>
                    <td>${size}</td>
                    <td style="font-family:'JetBrains Mono'; font-size:0.8rem;">${f.permissions}</td>
                    <td>${f.mod_time}</td>
                    <td style="text-align:right;">
                       ${!f.is_dir ? `<button class="btn-sm" onclick="openEditor(${ctid}, '${joinPath(currentPath, f.name)}')" title="Editar"><i class="fas fa-edit"></i></button>` : ''}
                    </td>
                `;
        tbody.appendChild(tr);
      });
    } else {
      const data = await res.json().catch(() => ({}));
      throw new Error(data.error || 'Erro ao carregar arquivos');
    }
  } catch (e) {
    console.error(e);
    loading.classList.add('hidden');
    error.classList.remove('hidden');
    showToast(e.message || 'Erro ao carregar arquivos', 'error');
  }
}

function updateBreadcrumbs(ctid) {
  const el = document.getElementById('file-breadcrumbs');
  if (!el) return;
  el.innerHTML = '';

  const parts = currentPath.split('/').filter(p => p);

  let buildPath = "";
  el.innerHTML += `<a href="#" onclick="loadFiles(${ctid}, '/'); return false;" style="color:var(--primary);">/</a>`;

  parts.forEach((p, i) => {
    buildPath += "/" + p;
    el.innerHTML += ` <span>/</span> <a href="#" onclick="loadFiles(${ctid}, '${buildPath}'); return false;">${p}</a>`;
  });
}

function navigateUp(ctid) {
  if (!ctid || currentPath === "/") return;
  const parts = currentPath.split('/').filter(p => p);
  parts.pop();
  const newPath = "/" + parts.join("/") || "/";
  loadFiles(ctid, newPath);
}

function joinPath(base, name) {
  return base === "/" ? "/" + name : base + "/" + name;
}

window.openEditor = async function (ctid, path) {
  const modal = document.getElementById('editor-modal');
  const contentArea = document.getElementById('editor-content');
  const title = document.getElementById('editor-filename');

  contentArea.value = "Carregando...";
  title.textContent = path;
  editorPath = path;
  editorContainerId = ctid;

  modal.classList.add('active');

  try {
    const res = await fetch(`${API}/vm/${ctid}/files/content?path=${encodeURIComponent(path)}`);
    if (res.ok) {
      const data = await res.json();
      contentArea.value = data.content;
    } else {
      contentArea.value = "Erro ao carregar arquivo.";
    }
  } catch (e) {
    contentArea.value = "Erro de conexão.";
  }
};

window.saveFile = async function () {
  const content = document.getElementById('editor-content').value;

  try {
    const res = await fetch(`${API}/vm/${editorContainerId}/files/content`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: editorPath, content: content })
    });

    if (res.ok) {
      alert("Arquivo salvo com sucesso!");
      closeEditorModal();
    } else {
      alert("Erro ao salvar arquivo.");
    }
  } catch (e) {
    alert("Erro de conexão.");
  }
};

window.closeEditorModal = function () {
  document.getElementById('editor-modal').classList.remove('active');
};
