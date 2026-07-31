document.addEventListener('DOMContentLoaded', () => {
  let allCards = [];
  let currentEditingCard = null;
  let draggedCardId = null;

  // DOM Elements
  const searchInput = document.getElementById('searchInput');
  const clearSearchBtn = document.getElementById('clearSearch');
  const filterType = document.getElementById('filterType');
  const filterPriority = document.getElementById('filterPriority');
  const sortBy = document.getElementById('sortBy');
  const btnNewCard = document.getElementById('btnNewCard');
  const btnImportCard = document.getElementById('btnImportCard');
  const btnAuditCards = document.getElementById('btnAuditCards');

  // Modal Elements
  const cardModal = document.getElementById('cardModal');
  const btnCloseModal = document.getElementById('btnCloseModal');
  const btnCancelModal = document.getElementById('btnCancelModal');
  const btnSaveCard = document.getElementById('btnSaveCard');
  const btnDeleteCard = document.getElementById('btnDeleteCard');

  const importCardModal = document.getElementById('importCardModal');
  const btnCloseImportCard = document.getElementById('btnCloseImportCard');
  const btnCancelImportCard = document.getElementById('btnCancelImportCard');
  const btnConfirmImportCard = document.getElementById('btnConfirmImportCard');
  const importCardText = document.getElementById('importCardText');
  const importCardMetrics = document.getElementById('importCardMetrics');
  const importCardError = document.getElementById('importCardError');

  const modalCardId = document.getElementById('modalCardId');
  const btnCopyModalPrompt = document.getElementById('btnCopyModalPrompt');
  const modalType = document.getElementById('modalType');
  const modalPriority = document.getElementById('modalPriority');
  const modalStatus = document.getElementById('modalStatus');
  const modalTitle = document.getElementById('modalTitle');
  const modalSummary = document.getElementById('modalSummary');
  const modalAreas = document.getElementById('modalAreas');
  const modalDescription = document.getElementById('modalDescription');
  const modalPrompt = document.getElementById('modalPrompt');
  const modalExpected = document.getElementById('modalExpected');
  const modalAuditContract = document.getElementById('modalAuditContract');
  const modalAuditReport = document.getElementById('modalAuditReport');
  const modalCompletionSummary = document.getElementById('modalCompletionSummary');
  const modalNotes = document.getElementById('modalNotes');

  const lblCreatedAt = document.getElementById('lblCreatedAt');
  const lblUpdatedAt = document.getElementById('lblUpdatedAt');
  const lblCompletedAt = document.getElementById('lblCompletedAt');

  const modalMarkdownContent = document.getElementById('modalMarkdownContent');
  const previewTokenMetrics = document.getElementById('previewTokenMetrics');
  const btnViewToggles = document.querySelectorAll('.btn-view-toggle');
  const cardModalWindow = document.querySelector('.card-modal-window');

  // Fetch Cards
  async function fetchCards() {
    try {
      const res = await fetch('/api/cards');
      if (res.ok) {
        allCards = await res.json();
        renderBoard();
      }
    } catch (err) {
      console.error('Erro ao carregar cards:', err);
    }
  }

  // Priority Weights for sorting
  const priorityWeights = {
    'Crítica': 4,
    'Alta': 3,
    'Média': 2,
    'Baixa': 1
  };

  // Render Kanban Board
  function renderBoard() {
    const query = searchInput.value.trim().lowerCase ? searchInput.value.trim().toLowerCase() : searchInput.value.trim();
    const typeFilter = filterType.value;
    const priorityFilter = filterPriority.value;
    const sortVal = sortBy.value;

    // Filter Cards
    let filtered = allCards.filter(c => {
      if (typeFilter && c.type !== typeFilter) return false;
      if (priorityFilter && c.priority !== priorityFilter) return false;
      if (query) {
        const textBlob = [
          c.id, c.title, c.summary, c.description, c.ai_prompt,
          c.expected_features, (c.impacted_areas || []).join(' '),
          c.type, c.status, c.completion_summary, c.notes_and_issues
        ].join(' ').toLowerCase();
        if (!textBlob.includes(query.toLowerCase())) return false;
      }
      return true;
    });

    // Sort Cards
    filtered.sort((a, b) => {
      if (sortVal === 'updated_desc') {
        return (b.updated_at || '').localeCompare(a.updated_at || '');
      } else if (sortVal === 'created_desc') {
        return (b.created_at || '').localeCompare(a.created_at || '');
      } else if (sortVal === 'priority_desc') {
        return (priorityWeights[b.priority] || 0) - (priorityWeights[a.priority] || 0);
      } else if (sortVal === 'id_asc') {
        return (a.id || '').localeCompare(b.id || '');
      }
      return 0;
    });

    // Group by status
    const statusLists = {
      'Ideias': [],
      'Pendente': [],
      'Em andamento': [],
      'Ajuste necessário': [],
      'Implementado': [],
      'Arquivado': []
    };

    filtered.forEach(card => {
      if (statusLists[card.status]) {
        statusLists[card.status].push(card);
      } else {
        statusLists['Pendente'].push(card);
      }
    });

    // Update Columns & Counts
    Object.keys(statusLists).forEach(status => {
      const container = document.getElementById(`list-${status}`);
      const countEl = document.getElementById(`count-${status}`);
      const cardsInStatus = statusLists[status];

      if (countEl) {
        countEl.textContent = cardsInStatus.length;
      }

      if (container) {
        container.innerHTML = '';

        if (cardsInStatus.length === 0) {
          container.innerHTML = `<div class="empty-state" style="color:var(--text-dim); font-size:12px; text-align:center; padding:16px;">Vazio</div>`;
        } else {
          cardsInStatus.forEach(card => {
            container.appendChild(createCardElement(card));
          });
        }
      }
    });
  }

  // Create Card DOM Element (Kanban View)
  function createCardElement(card) {
    const el = document.createElement('div');
    el.className = 'task-card';
    el.setAttribute('draggable', 'true');
    el.setAttribute('data-id', card.id);
    const tokenMetrics = card.markdown_token_metrics;
    el.innerHTML = `
      <div class="card-header-row">
        <span class="card-id">${escapeHtml(card.id)}</span>
        <div class="card-badges">
          <span class="badge badge-type" data-type="${escapeHtml(card.type)}">${escapeHtml(card.type)}</span>
          <span class="badge badge-priority" data-priority="${escapeHtml(card.priority)}">${escapeHtml(card.priority)}</span>
        </div>
      </div>
      <div class="card-title">${escapeHtml(card.title)}</div>
      <div class="card-summary">${escapeHtml(card.summary || '')}</div>
      ${tokenMetrics ? renderTokenMetrics(tokenMetrics) : ''}
      <div class="card-footer-row">
        <span>🕒 ${escapeHtml(card.updated_at ? card.updated_at.split(' ')[0] : '')}</span>
        <button type="button" class="btn-copy-card-prompt" title="Copy AI prompt for task-board/data/cards/${escapeHtml(card.id)}.md">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
            <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
          </svg>
          <span>Copy Prompt</span>
        </button>
      </div>
    `;

    const copyBtn = el.querySelector('.btn-copy-card-prompt');
    if (copyBtn) {
      copyBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        const promptText = buildCardPromptText(card);
        const copiedHTML = `
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="#10b981" stroke-width="2">
            <polyline points="20 6 9 17 4 12"></polyline>
          </svg>
          <span style="color: #10b981;">Copied!</span>
        `;
        await copyPrompt(copyBtn, promptText, 'Copied!', copiedHTML);
      });
    }

    // Click to Open Modal
    el.addEventListener('click', (e) => {
      openEditModal(card);
    });

    // Drag Events
    el.addEventListener('dragstart', (e) => {
      draggedCardId = card.id;
      el.classList.add('dragging');
      e.dataTransfer.setData('text/plain', card.id);
    });

    el.addEventListener('dragend', () => {
      el.classList.remove('dragging');
      draggedCardId = null;
    });

    return el;
  }

  function renderTokenMetrics(metrics) {
    const profiles = metrics.profiles || {};
    const formatTokens = (profile) => profile && Number(profile.tokens || 0).toLocaleString('en-US');
    const title = `Complete Markdown file: ${Number(metrics.characters || 0).toLocaleString('en-US')} characters, ${Number(metrics.lines || 0).toLocaleString('en-US')} lines. Token values are estimates; use the provider tokenizer for exact counts.`;
    return `
      <div class="card-token-metrics" title="${escapeHtml(title)}">
        <span class="card-token-file">Full .md</span>
        <span>Codex ${escapeHtml(formatTokens(profiles.codex) || '—')}</span>
        <span>Claude ${escapeHtml(formatTokens(profiles.claude) || '—')}</span>
        <span>Gemini ${escapeHtml(formatTokens(profiles.gemini) || '—')}</span>
        <span>AGY ${escapeHtml(formatTokens(profiles.agy) || '—')}</span>
      </div>`;
  }

  // Setup Column Drag & Drop
  document.querySelectorAll('.kanban-column').forEach(col => {
    col.addEventListener('dragover', (e) => {
      e.preventDefault();
      col.classList.add('drag-over');
    });

    col.addEventListener('dragleave', () => {
      col.classList.remove('drag-over');
    });

    col.addEventListener('drop', async (e) => {
      e.preventDefault();
      col.classList.remove('drag-over');
      const targetStatus = col.getAttribute('data-status');
      
      if (draggedCardId && targetStatus) {
        const card = allCards.find(c => c.id === draggedCardId);
        if (card && card.status !== targetStatus) {
          await updateCardStatus(card, targetStatus);
        }
      }
    });
  });

  // Toggle Collapsed Columns
  const toggleImpl = document.getElementById('toggle-Implementado');
  if (toggleImpl) {
    toggleImpl.addEventListener('click', (e) => {
      document.getElementById('col-Implementado').classList.toggle('collapsed');
    });
  }

  const toggleArq = document.getElementById('toggle-Arquivado');
  if (toggleArq) {
    toggleArq.addEventListener('click', (e) => {
      document.getElementById('col-Arquivado').classList.toggle('collapsed');
    });
  }

  // Open Create Modal
  btnNewCard.addEventListener('click', () => {
    currentEditingCard = null;
    modalCardId.textContent = 'TASK-NOVO';
    modalTitle.value = '';
    modalSummary.value = '';
    modalAreas.value = '';
    modalDescription.value = '';
    modalPrompt.value = '';
    modalExpected.value = '';
    modalAuditContract.value = '';
    modalAuditReport.value = '';
    modalCompletionSummary.value = '';
    modalNotes.value = '';

    modalType.value = 'Implementação';
    modalPriority.value = 'Média';
    modalStatus.value = 'Pendente';

    lblCreatedAt.textContent = 'Criado: Agora';
    lblUpdatedAt.textContent = 'Atualizado: Agora';
    lblCompletedAt.textContent = 'Implementado: -';

    btnDeleteCard.style.display = 'none';
    if (btnCopyModalPrompt) btnCopyModalPrompt.style.display = 'none';
    adjustModalViewForScreen();
    updateLiveMarkdownPreview();
    cardModal.style.display = 'flex';
  });

  function closeImportCardModal() {
    importCardModal.style.display = 'none';
    importCardMetrics.hidden = true;
    importCardMetrics.textContent = '';
    importCardError.hidden = true;
    importCardError.textContent = '';
  }

  function showImportError(message) {
    importCardError.textContent = message;
    importCardError.hidden = false;
  }

  function updateImportPreview() {
    if (!window.TaskBoardCardImporter) {
      importCardMetrics.hidden = true;
      btnConfirmImportCard.disabled = true;
      showImportError('Card importer is unavailable. Reload the Task Board and try again.');
      return null;
    }

    const parsed = window.TaskBoardCardImporter.parse(importCardText.value);
    const { metrics } = parsed;
    importCardMetrics.textContent = `Complete Card: ${metrics.tokens.toLocaleString('en-US')} conservative estimated tokens · ${metrics.words.toLocaleString('en-US')} words · ${metrics.characters.toLocaleString('en-US')} characters · Size: ${metrics.classification}`;
    importCardMetrics.hidden = false;
    btnConfirmImportCard.disabled = !parsed.valid;

    if (parsed.valid) {
      importCardError.hidden = true;
      importCardError.textContent = '';
    } else {
      showImportError(parsed.errors.join(' '));
    }
    return parsed;
  }

  btnImportCard.addEventListener('click', () => {
    importCardText.value = '';
    importCardError.hidden = true;
    importCardMetrics.hidden = true;
    btnConfirmImportCard.disabled = true;
    importCardModal.style.display = 'flex';
    importCardText.focus();
  });

  importCardText.addEventListener('input', updateImportPreview);

  btnCloseImportCard.addEventListener('click', closeImportCardModal);
  btnCancelImportCard.addEventListener('click', closeImportCardModal);
  importCardModal.addEventListener('click', (e) => {
    if (e.target === importCardModal) closeImportCardModal();
  });

  btnConfirmImportCard.addEventListener('click', async () => {
    const parsed = updateImportPreview();
    if (!parsed || !parsed.valid) {
      return;
    }

    btnConfirmImportCard.disabled = true;
    try {
      const res = await fetch('/api/cards', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(parsed.card)
      });
      if (!res.ok) {
        showImportError('Unable to create the imported Card.');
        return;
      }
      closeImportCardModal();
      fetchCards();
    } catch (err) {
      console.error('Erro ao importar Card:', err);
      showImportError('Unable to import the Card.');
    } finally {
      btnConfirmImportCard.disabled = false;
    }
  });

  btnAuditCards.addEventListener('click', async () => {
    if (!confirm('Run deterministic audits for every configured Card?')) return;
    btnAuditCards.disabled = true;
    try {
      const res = await fetch('/api/audit', { method: 'POST' });
      if (!res.ok) throw new Error('Audit request failed');
      const audited = await res.json();
      const passed = audited.filter((item) => item.passed).length;
      const configured = audited.filter((item) => item.configured).length;
      alert(`Audit complete: ${passed}/${configured} configured Cards passed.`);
      fetchCards();
    } catch (err) {
      console.error('Card audit failed:', err);
      alert('Unable to run the Card audit.');
    } finally {
      btnAuditCards.disabled = false;
    }
  });

  // Live Markdown Preview Functions
  function updateLiveMarkdownPreview() {
    if (!modalMarkdownContent) return;

    const id = currentEditingCard ? currentEditingCard.id : (modalCardId.textContent || 'TASK-NOVO');
    const title = modalTitle.value.trim() || 'Sem Título';
    const type = modalType.value || 'Implementação';
    const priority = modalPriority.value || 'Média';
    const status = modalStatus.value || 'Pendente';
    const areas = modalAreas.value.split(',').map(s => s.trim()).filter(Boolean);
    const summary = modalSummary.value.trim() || title;
    const description = modalDescription.value.trim();
    const prompt = modalPrompt.value.trim();
    const expected = modalExpected.value.trim();
    const auditContract = modalAuditContract.value.trim();
    const auditReport = modalAuditReport.value.trim();
    const completionSummary = modalCompletionSummary.value.trim();
    const notes = modalNotes.value.trim();
    const createdAt = currentEditingCard ? (currentEditingCard.created_at || 'Agora') : 'Agora';
    const updatedAt = currentEditingCard ? (currentEditingCard.updated_at || 'Agora') : 'Agora';

    const frontmatterHtml = `
      <div class="md-frontmatter-card">
        <div class="md-fm-badge-row">
          <span class="card-id">${escapeHtml(id)}</span>
          <span class="badge badge-type" data-type="${escapeHtml(type)}">${escapeHtml(type)}</span>
          <span class="badge badge-priority" data-priority="${escapeHtml(priority)}">${escapeHtml(priority)}</span>
          <span class="badge badge-status" style="background:var(--bg-input); border:1px solid var(--border-color); color:var(--text-main); font-size:10px; font-weight:600; padding:2px 6px; border-radius:4px;">${escapeHtml(status)}</span>
        </div>
        ${areas.length > 0 ? `<div style="font-size:12px; color:var(--text-muted); margin-top:4px;"><strong>Áreas:</strong> ${escapeHtml(areas.join(', '))}</div>` : ''}
        <div class="md-fm-meta">
          <span>Criado: ${escapeHtml(createdAt)}</span>
          <span>Atualizado: ${escapeHtml(updatedAt)}</span>
        </div>
      </div>
    `;

    let mdBody = `# ${id} — ${title}\n\n`;
    if (summary) mdBody += `## Summary\n\n${summary}\n\n`;
    if (description) mdBody += `## Detailed Description\n\n${description}\n\n`;
    if (prompt) mdBody += `## Detailed AI Prompt\n\n${prompt}\n\n`;
    if (expected) mdBody += `## Expected Features\n\n${expected}\n\n`;
    if (auditContract) mdBody += `## Audit Contract\n\n\`\`\`text\n${auditContract}\n\`\`\`\n\n`;
    if (auditReport) mdBody += `## Audit Report\n\n\`\`\`text\n${auditReport}\n\`\`\`\n\n`;
    if (completionSummary) mdBody += `## Completion Summary\n\n${completionSummary}\n\n`;
    if (notes) mdBody += `## Notes and Issues\n\n${notes}\n\n`;

    const formattedHtml = parseSimpleMarkdown(mdBody);
    modalMarkdownContent.innerHTML = frontmatterHtml + formattedHtml;

    if (previewTokenMetrics) {
      const fullMdString = generateFullCardMdString({
        id, title, summary, type, priority, status, created_at: createdAt, updated_at: updatedAt,
        impacted_areas: areas, description, ai_prompt: prompt, expected_features: expected,
        audit_contract: auditContract, audit_report: auditReport, completion_summary: completionSummary, notes_and_issues: notes
      });
      const len = fullMdString.length;
      const approxTokens = Math.ceil(len / 3.8);
      previewTokenMetrics.textContent = `${approxTokens.toLocaleString('en-US')} conservative tokens · ${len.toLocaleString('en-US')} chars`;
    }
  }

  function generateFullCardMdString(c) {
    const lines = ["---"];
    lines.push(`id: ${c.id}`);
    lines.push(`title: ${c.title}`);
    lines.push(`summary: ${c.summary}`);
    lines.push(`type: ${c.type}`);
    lines.push(`status: ${c.status}`);
    lines.push(`priority: ${c.priority}`);
    lines.push(`created_at: ${c.created_at}`);
    lines.push(`updated_at: ${c.updated_at}`);
    lines.push("impacted_areas:");
    (c.impacted_areas || []).forEach(a => lines.push(`  - ${a}`));
    lines.push("---", "", `# ${c.id} — ${c.title}`, "");
    if (c.summary) lines.push("## Summary", "", c.summary, "");
    if (c.description) lines.push("## Detailed Description", "", "<!-- task-board:description:start -->", c.description, "<!-- task-board:description:end -->", "");
    if (c.ai_prompt) lines.push("## Detailed AI Prompt", "", "<!-- task-board:ai-prompt:start -->", c.ai_prompt, "<!-- task-board:ai-prompt:end -->", "");
    if (c.expected_features) lines.push("## Expected Features", "", "<!-- task-board:expected-features:start -->", c.expected_features, "<!-- task-board:expected-features:end -->", "");
    if (c.audit_contract) lines.push("## Audit Contract", "", "<!-- task-board:audit-contract:start -->", c.audit_contract, "<!-- task-board:audit-contract:end -->", "");
    if (c.audit_report) lines.push("## Audit Report", "", "<!-- task-board:audit-report:start -->", c.audit_report, "<!-- task-board:audit-report:end -->", "");
    if (c.completion_summary) lines.push("## Completion Summary", "", "<!-- task-board:completion-summary:start -->", c.completion_summary, "<!-- task-board:completion-summary:end -->", "");
    if (c.notes_and_issues) lines.push("## Notes and Issues", "", "<!-- task-board:notes-and-issues:start -->", c.notes_and_issues, "<!-- task-board:notes-and-issues:end -->", "");
    return lines.join("\n");
  }

  function buildCardPromptText(c) {
    const fullMd = generateFullCardMdString(c);
    const cardId = c.id || 'TASK-NOVO';
    const relPath = `task-board/data/cards/${cardId}.md`;
    const footer = [
      "---",
      "Origem: Sistema de Tasks do NodeStage",
      `Arquivo de Referência: ${relPath}`,
      "Instrução: Execute a tarefa descrita acima no repositório NodeStage respeitando integralmente as regras, arquitetura, restrições e critérios descritos."
    ].join("\n");
    return fullMd.trim() + "\n\n" + footer;
  }

  function parseSimpleMarkdown(mdText) {
    let text = mdText.replace(/<!--\s*task-board:[^>]+-->/g, '');

    const codeBlocks = [];
    text = text.replace(/```([\s\S]*?)```/g, (match, code) => {
      codeBlocks.push(code.trim());
      return `___CODE_BLOCK_${codeBlocks.length - 1}___`;
    });

    const lines = text.split('\n');
    let html = '';
    let inList = false;

    lines.forEach(line => {
      let trimmed = line.trim();

      if (trimmed.startsWith('___CODE_BLOCK_') && trimmed.endsWith('___')) {
        if (inList) { html += '</ul>'; inList = false; }
        const idx = parseInt(trimmed.replace('___CODE_BLOCK_', '').replace('___', ''), 10);
        html += `<pre><code>${escapeHtml(codeBlocks[idx] || '')}</code></pre>`;
        return;
      }

      if (trimmed.startsWith('- ') || trimmed.startsWith('* ')) {
        if (!inList) { html += '<ul>'; inList = true; }
        html += `<li>${formatInlineMarkdown(trimmed.substring(2))}</li>`;
        return;
      } else if (inList && trimmed === '') {
        html += '</ul>';
        inList = false;
        return;
      }

      if (inList && !trimmed.startsWith('- ') && !trimmed.startsWith('* ')) {
        html += '</ul>';
        inList = false;
      }

      if (trimmed.startsWith('# ')) {
        html += `<h1>${formatInlineMarkdown(trimmed.substring(2))}</h1>`;
      } else if (trimmed.startsWith('## ')) {
        html += `<h2>${formatInlineMarkdown(trimmed.substring(3))}</h2>`;
      } else if (trimmed.startsWith('### ')) {
        html += `<h3>${formatInlineMarkdown(trimmed.substring(4))}</h3>`;
      } else if (trimmed === '---') {
        html += '<hr>';
      } else if (trimmed !== '') {
        html += `<p>${formatInlineMarkdown(trimmed)}</p>`;
      }
    });

    if (inList) html += '</ul>';
    return html;
  }

  function formatInlineMarkdown(str) {
    let s = escapeHtml(str);
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    s = s.replace(/\*([^*]+)\*/g, '<em>$1</em>');
    s = s.replace(/`([^`]+)`/g, '<code>$1</code>');
    return s;
  }

  // Bind Input Change Events for Live Markdown Preview
  [modalTitle, modalSummary, modalAreas, modalDescription, modalPrompt, modalExpected, modalAuditContract, modalAuditReport, modalCompletionSummary, modalNotes, modalType, modalPriority, modalStatus].forEach(field => {
    if (field) {
      field.addEventListener('input', updateLiveMarkdownPreview);
      field.addEventListener('change', updateLiveMarkdownPreview);
    }
  });

  // Bind View Mode Toggles
  btnViewToggles.forEach(btn => {
    btn.addEventListener('click', () => {
      const view = btn.getAttribute('data-view');
      btnViewToggles.forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      if (cardModalWindow) {
        cardModalWindow.setAttribute('data-view', view);
      }
    });
  });

  function adjustModalViewForScreen() {
    if (!cardModalWindow) return;
    const defaultView = window.innerWidth <= 768 ? 'edit' : 'split';
    cardModalWindow.setAttribute('data-view', defaultView);
    btnViewToggles.forEach(b => {
      b.classList.toggle('active', b.getAttribute('data-view') === defaultView);
    });
  }

  // Open Edit Modal
  function openEditModal(card) {
    currentEditingCard = card;
    modalCardId.textContent = card.id;
    modalTitle.value = card.title || '';
    modalSummary.value = card.summary || '';
    modalAreas.value = (card.impacted_areas || []).join(', ');
    modalDescription.value = card.description || '';
    modalPrompt.value = card.ai_prompt || '';
    modalExpected.value = card.expected_features || '';
    modalAuditContract.value = card.audit_contract || '';
    modalAuditReport.value = card.audit_report || '';
    modalCompletionSummary.value = card.completion_summary || '';
    modalNotes.value = card.notes_and_issues || '';

    modalType.value = card.type || 'Implementação';
    modalPriority.value = card.priority || 'Média';
    modalStatus.value = card.status || 'Pendente';

    lblCreatedAt.textContent = `Criado: ${card.created_at || 'N/A'}`;
    lblUpdatedAt.textContent = `Atualizado: ${card.updated_at || 'N/A'}`;
    lblCompletedAt.textContent = `Implementado: ${card.completed_at || 'Não'}`;

    btnDeleteCard.style.display = 'block';
    if (btnCopyModalPrompt) btnCopyModalPrompt.style.display = 'inline-flex';
    adjustModalViewForScreen();
    updateLiveMarkdownPreview();
    cardModal.style.display = 'flex';
  }

  async function writeClipboard(text) {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return;
    }
    const fallback = document.createElement('textarea');
    fallback.value = text;
    fallback.setAttribute('readonly', '');
    fallback.style.position = 'fixed';
    fallback.style.opacity = '0';
    document.body.appendChild(fallback);
    fallback.select();
    const copied = document.execCommand('copy');
    fallback.remove();
    if (!copied) throw new Error('Clipboard is unavailable');
  }

  async function copyPrompt(button, text, copiedLabel, copiedHTML = null) {
    const originalHTML = button.innerHTML;
    try {
      await writeClipboard(text);
      if (copiedHTML) {
        button.innerHTML = copiedHTML;
      } else {
        button.textContent = copiedLabel;
      }
      setTimeout(() => { button.innerHTML = originalHTML; }, 1800);
    } catch (err) {
      console.error('Erro ao copiar para a área de transferência:', err);
      alert('Unable to copy the requested content.');
    }
  }

  if (btnCopyModalPrompt) {
    btnCopyModalPrompt.addEventListener('click', async () => {
      const id = currentEditingCard ? currentEditingCard.id : (modalCardId.textContent || 'TASK-NOVO');
      const cardObj = {
        id: id,
        title: modalTitle.value.trim() || 'Sem Título',
        summary: modalSummary.value.trim() || modalTitle.value.trim(),
        type: modalType.value,
        priority: modalPriority.value,
        status: modalStatus.value,
        created_at: currentEditingCard ? (currentEditingCard.created_at || 'Agora') : 'Agora',
        updated_at: currentEditingCard ? (currentEditingCard.updated_at || 'Agora') : 'Agora',
        completed_at: currentEditingCard ? currentEditingCard.completed_at : null,
        impacted_areas: modalAreas.value.split(',').map(s => s.trim()).filter(Boolean),
        description: modalDescription.value.trim(),
        ai_prompt: modalPrompt.value.trim(),
        expected_features: modalExpected.value.trim(),
        audit_contract: modalAuditContract.value.trim(),
        audit_report: modalAuditReport.value.trim(),
        completion_summary: modalCompletionSummary.value.trim(),
        notes_and_issues: modalNotes.value.trim()
      };

      const promptText = buildCardPromptText(cardObj);
      const copiedHTML = `
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="#10b981" stroke-width="2">
          <polyline points="20 6 9 17 4 12"></polyline>
        </svg>
        <span style="color: #10b981;">Copied!</span>
      `;
      await copyPrompt(btnCopyModalPrompt, promptText, 'Copied!', copiedHTML);
    });
  }

  // Close Modal
  function closeModal() {
    cardModal.style.display = 'none';
    currentEditingCard = null;
  }

  btnCloseModal.addEventListener('click', closeModal);
  btnCancelModal.addEventListener('click', closeModal);

  cardModal.addEventListener('click', (e) => {
    if (e.target === cardModal) closeModal();
  });

  // Save Card
  btnSaveCard.addEventListener('click', async () => {
    const title = modalTitle.value.trim();
    if (!title) {
      alert('Por favor, informe o título do card.');
      return;
    }

    const impactedAreas = modalAreas.value.split(',').map(s => s.trim()).filter(Boolean);

    const payload = {
      title: title,
      summary: modalSummary.value.trim() || title,
      description: modalDescription.value.trim() || title,
      ai_prompt: modalPrompt.value.trim(),
      expected_features: modalExpected.value.trim(),
      audit_contract: modalAuditContract.value.trim(),
      impacted_areas: impactedAreas,
      type: modalType.value,
      priority: modalPriority.value,
      status: modalStatus.value,
      completion_summary: modalCompletionSummary.value.trim() || null,
      notes_and_issues: modalNotes.value.trim() || null
    };

    try {
      if (currentEditingCard) {
        // PUT Update
        const res = await fetch(`/api/cards/${currentEditingCard.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        if (res.ok) {
          closeModal();
          fetchCards();
        } else {
          alert('Erro ao atualizar card');
        }
      } else {
        // POST Create
        const res = await fetch('/api/cards', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        if (res.ok) {
          closeModal();
          fetchCards();
        } else {
          alert('Erro ao criar card');
        }
      }
    } catch (err) {
      console.error('Erro ao salvar card:', err);
    }
  });

  // Delete Card
  btnDeleteCard.addEventListener('click', async () => {
    if (!currentEditingCard) return;
    if (confirm(`Tem certeza que deseja excluir o card ${currentEditingCard.id}?`)) {
      try {
        const res = await fetch(`/api/cards/${currentEditingCard.id}`, {
          method: 'DELETE'
        });
        if (res.ok) {
          closeModal();
          fetchCards();
        }
      } catch (err) {
        console.error('Erro ao excluir card:', err);
      }
    }
  });

  // Quick Status Update via Drag and Drop
  async function updateCardStatus(card, newStatus) {
    const payload = {
      ...card,
      status: newStatus
    };

    try {
      const res = await fetch(`/api/cards/${card.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });

      if (res.ok) {
        fetchCards();
      }
    } catch (err) {
      console.error('Erro ao atualizar status:', err);
    }
  }

  // Filter & Search Event Listeners
  searchInput.addEventListener('input', () => {
    clearSearchBtn.style.display = searchInput.value ? 'block' : 'none';
    renderBoard();
  });

  clearSearchBtn.addEventListener('click', () => {
    searchInput.value = '';
    clearSearchBtn.style.display = 'none';
    renderBoard();
  });

  filterType.addEventListener('change', renderBoard);
  filterPriority.addEventListener('change', renderBoard);
  sortBy.addEventListener('change', renderBoard);

  // Helper HTML escaper
  function escapeHtml(str) {
    if (!str) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  // Copy Prompt Rule Button
  const btnCopyPromptRule = document.getElementById('btnCopyPromptRule');
  const OPTIMIZED_CARD_CREATION_TEMPLATE = `Você é o gerador oficial de Cards do Task Board do NodeStage. Sua função é transformar a solicitação fornecida em um Card técnico preciso, enxuto, não destrutivo e diretamente operacional para uma IA de programação.

Retorne SOMENTE os campos editoriais definidos em “Formato de saída”, na ordem e títulos exatos. Não implemente a tarefa, não invente detalhes técnicos ou de produto e não omita nenhuma exigência única.

---

### 1. Regra Fundamental do Tamanho e Orçamento de Tokens

- O Card deve ter o MENOR tamanho necessário para conter todos os requisitos únicos, riscos e orientações. Comprimento não é critério de qualidade e não existe meta mínima de tokens.
- O Card completo (soma de todas as seções) enviará contexto a uma IA de programação.
- Limite máximo absoluto: 9.000 tokens. Se um Card ultrapassar 9.000 tokens, ele precisará obrigatoriamente de nova otimização.
- Faixas de referência de tamanho:
  - Até 4.000 tokens: Compacto (tarefas pequenas ou direcionadas).
  - 4.001 a 6.000 tokens: Adequado (tarefas de média complexidade).
  - 6.001 a 9.000 tokens: Detalhado (tarefas complexas ou de alta integração).
- Classificação de complexidade:
  - Pequena: alteração pontual e localizada, sem integração ampla, migração ou persistência complexa.
  - Média: alteração envolvendo múltiplos estados, interface, persistência ou integração entre sistemas.
  - Complexa: mudança arquitetural central, múltiplos fluxos, concorrência, migração ou alto risco de regressão.
- A prioridade ou urgência da tarefa NUNCA determina o tamanho do Card.
- Otimizar ou reduzir tamanho NUNCA autoriza remover, enfraquecer, adaptar livremente ou omitir qualquer requisito único original.

---

### 2. Hierarquia e Responsabilidade Única das Seções

Cada requisito único da solicitação deve aparecer UMA ÚNICA VEZ no Card.

1. Descrição Detalhada (Fonte Canônica Única)
   - É a única fonte da verdade de TODOS os requisitos funcionais, regras de negócio, comportamentos, estados de interface, persistência, sincronização, compatibilidade, fluxos de dados, entradas/saídas e casos de exceção.
   - Todos os requisitos originais devem ser registrados aqui com precisão direta.

2. Prompt Detalhado para a IA (Orientação de Execução)
   - É estritamente operacional e direcionado à IA que implementará o código. NÃO é uma segunda especificação funcional.
   - Deve referenciar a Descrição Detalhada como fonte canônica.
   - Contém APENAS: ordem de investigação no repositório, sistemas a localizar, estratégia de integração, divisão de responsabilidades, prevenção de regressões, tratamento técnico de erros, restrições de escopo e orientação para o relatório final.
   - PROIBIDO REPETIR na seção de Prompt os requisitos funcionais, posições visuais, estados ou regras de negócio já documentados na Descrição Detalhada.

3. Funcionalidades Esperadas (Checklist de Aceitação)
   - Lista curta e objetiva dos principais resultados verificáveis.
   - NÃO deve explicar arquitetura, NÃO deve repetir a Descrição Detalhada, NÃO deve reescrever os critérios de aceitação e NÃO deve introduzir novos comportamentos.

4. Contrato de Auditoria (Opcional)
   - Omitir completamente se não houver verificações determinísticas de código.

---

### 3. Regra de Não Invenção de Decisões de Produto e Arquitetura

- Quando a solicitação não definir elementos como valores padrão, precedência de configurações, herança, política de conflito/cancelamento/migração, limites numéricos ou locais de UI adicionais, PROIBIDO inventar uma decisão silenciosa.
- Em caso de omissão na solicitação, oriente a IA de implementação a:
  1. Localizar o padrão equivalente existente no repositório;
  2. Preservar o comportamento atual onde aplicável;
  3. Adotar a menor decisão compatível com a arquitetura encontrada;
  4. Não criar novas configurações sem necessidade;
  5. Registrar no relatório final qualquer decisão adotada.
- Se a decisão for puramente de produto e não puder ser derivada da arquitetura, registre-a como decisão pendente na Descrição Detalhada sem inventar uma resposta.

---

### 4. Distinção entre Fatos, Hipóteses e Análise de Código

- Trate como fato APENAS informações explicitamente fornecidas na solicitação.
- Nomes de classes, arquivos, funções, estruturas ou serviços não confirmados devem ser tratados como desconhecidos.
- Use formulações neutras como “localizar o proprietário atual de [responsabilidade]” em vez de assumir nomes de classes ou componentes não informados.
- Exemplo Incorreto: “Alterar o PlaybackManager para armazenar o BPM.”
- Exemplo Correto: “Localizar o proprietário atual de playback e tempo e integrar a gestão de BPM nessa responsabilidade, reaproveitando a estrutura existente.”
- Exija sempre que a IA analise o código real antes de escolher a solução de integração.

---

### 5. Detalhamento Técnico Permitido vs Invenção Arquitetural

- Requisitos técnicos derivados só podem ser incluídos se forem INDISPENSÁVEIS para garantir: consistência interna, segurança, atomicidade, cancelamento limpo, recuperação de falha, prevenção de duplicidade, validade de estado ou persistência íntegra.
- Exigências derivadas devem ser escritas de forma neutra e arquiteturalmente transparente.
- PROIBIDO INVENTAR: frameworks, bibliotecas, bancos de dados, protocolos, formatos de arquivo, threads, serviços, diretórios ou mecanismos de eventos não mencionados na solicitação.

---

### 6. Restrições aos Requisitos Derivados

- NÃO são considerados requisitos derivados: melhorias de conveniência, recursos adjacentes, expansões futuras, refatorações opcionais, sugestões estéticas não solicitadas ou otimizações sem relação direta com o problema.
- Nenhuma funcionalidade opcional ou “interessante” pode ser incluída como se fosse um requisito necessário.

---

### 7. Processo Obrigatório de Deduplicação antes da Resposta

Antes de emitir o Card, execute a seguinte verificação interna:
1. Extraia todos os requisitos únicos da solicitação.
2. Atribua UMA localização canônica para cada requisito (na Descrição Detalhada).
3. Elimine repetições literais e equivalências semânticas (ex: “não disparar duas vezes” e “evitar requisições duplicadas” são a mesma regra e devem aparecer uma única vez).
4. Verifique se o Prompt Detalhado para a IA repete trechos da Descrição Detalhada.
5. Verifique se Funcionalidades Esperadas repete os critérios.
6. Confirme que nenhuma decisão não informada ou recurso opcional foi inventado.
7. Assegure que o Contrato de Auditoria só contenha verificações determinísticas válidas ou seja omitido.
8. Confirme que todos os nomes de UI, IDs, caminhos, comandos e termos técnicos foram preservados literalmente.

---

### 8. Preenchimento de Seções e Conteúdo Genérico

- PROIBIDO preencher seções com texto genérico, explicações redundantes ou parágrafos artificiais apenas para ocupar o formato.
- Se uma seção não possuir conteúdo adicional além do que consta na Descrição Detalhada, utilize uma indicação direta (ex: “Sem requisitos adicionais além da Descrição Detalhada.”).

---

### 9. Validação e Critérios de Aceitação Objetivos

- Os critérios de aceitação devem ser condições objetivas que permitam avaliação direta: passou, falhou ou não aplicável.
- PROIBIDO usar termos vagos ou subjetivos como: “funcionar perfeitamente”, “manter boa performance”, “revisar completamente”, “seguir boas práticas” ou “integrar adequadamente”.
- Validação por tipo de tarefa:
  - Lógica / Regras / Persistência: exigir testes automatizados quando houver infraestrutura no projeto; testar limites, falhas e recuperação.
  - Interface Visual: indicar estados visuais, resoluções e fluxos específicos a verificar manualmente.
  - Integração: validar fluxo principal, tratamento de indisponibilidade e regressão.

---

### 10. Restrições e Não Objetivos Justificados

- Inclua restrições/não objetivos APENAS quando: tiverem sido informados pelo usuário, evitarem uma expansão provável de escopo ou protegerem uma compatibilidade existente contra uma solução incorreta provável.

---

### 11. Preservação Literal de Termos

- Mantenha exatamente como fornecidos: nomes próprios, labels de UI, IDs, caminhos, extensões, formatos, comandos, valores, nomes de menus/abas/componentes e termos técnicos.

---

### 12. Separação de Formato Editorial vs Arquivo Persistido

- O retorno deve conter APENAS os campos editoriais solicitados no “Formato de saída”.
- NÃO inclua frontmatter YAML (---), ID do card, status, datas, comentários HTML ou cabeçalhos de origem fora da estrutura definida.

---

### 13. Regras do Contrato de Auditoria

- Opcional. Se incluído, aceita SOMENTE linhas determinísticas no formato:
  - - source: caminho/relativo contains texto literal
  - - registry: caminho/relativo
  - - validation: code-registry-validate|change-contract|task-board-markdown-storage|task-board-card-importer
- NUNCA inclua comandos livres de terminal, caminhos inventados, textos literais não confirmados ou instruções em linguagem natural.

---

### 14. Ordem de Precedência Inegociável

1. Solicitação original do usuário.
2. Requisitos únicos na Descrição Detalhada.
3. Regras obrigatórias e restrições específicas.
4. Organização do Prompt Detalhado para a IA.
5. Formato padronizado.
6. Otimização de tamanho.

Nenhuma regra de nível inferior pode alterar, mitigar ou contradizer uma regra de nível superior.

---

### 15. Orientação para o Relatório Final da Implementação

Na seção Instrução de execução do Prompt, oriente a IA de programação a finalizar com um relatório objetivo contendo:
- Diagnóstico do estado encontrado;
- Arquivos alterados;
- Decisões arquiteturais adotadas;
- Comportamentos implementados;
- Testes e validações executados;
- Regressões verificadas;
- Limitações reais ou pendências;
- Atualização de versão (VERSION), CHANGELOG.md e registros obrigatórios do projeto (Code Registry).

---

Formato de saída:

- Título Direto: [título objetivo]
- Resumo de Uma Frase: [contexto curto e motivo da mudança]
- Tipo: [Ideia | Implementação | Melhoria | Ajuste | Bug | Correção]
- Prioridade: [Baixa | Média | Alta | Crítica]
- Áreas Envolvidas: [lista separada por vírgulas]

- Descrição Detalhada:
[registro completo, único e não destrutivo de todos os requisitos da solicitação]

- Prompt Detalhado para a IA:
# Tarefa
[título]
## Contexto
[resumo curto]
## Objetivo
[resultado final esperado]
## Estado atual e investigação
[o que investigar no código, sistemas a localizar e responsabilidades a mapear]
## Estratégia de integração e regras
[ordem de execução, prevenção de regressões e divisão de responsabilidades de acordo com a Descrição Detalhada]
## Validação e tratamento de erros
[testes exigidos, tratamento de falhas e condições de contorno]
## Restrições de escopo
[o que não alterar e o que não expandir]
## Instrução de execução e relatório
[orientação para analisar o código real antes de alterar e roteiro do relatório final de entrega]

- Funcionalidades Esperadas:
[checklist curta de aceitação com resultados principais verificáveis]

- Contrato de Auditoria:
[opcional; omitir se não houver verificações determinísticas reais no formato '- source: ...', '- registry: ...' ou '- validation: ...']

Solicitação a transformar em Card:
[COLE AQUI A SOLICITAÇÃO ORIGINAL]`;
  if (btnCopyPromptRule) {
    btnCopyPromptRule.addEventListener('click', async () => {
      try {
        await writeClipboard(OPTIMIZED_CARD_CREATION_TEMPLATE);
        const originalHTML = btnCopyPromptRule.innerHTML;
        btnCopyPromptRule.innerHTML = `
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#10b981" stroke-width="2">
            <polyline points="20 6 9 17 4 12"></polyline>
          </svg>
          Copiado!
        `;
        setTimeout(() => {
          btnCopyPromptRule.innerHTML = originalHTML;
        }, 2000);
      } catch (err) {
        console.error('Erro ao copiar para a área de transferência:', err);
      }
    });
  }

  // Initial Load
  fetchCards();
});
