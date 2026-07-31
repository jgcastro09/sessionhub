/* Parses the complete Card format produced by the global prompt rule. */
(function (root) {
  'use strict';

  const FIELD_BY_LABEL = new Map([
    ['titulo direto', 'title'],
    ['resumo de uma frase', 'summary'],
    ['tipo', 'type'],
    ['prioridade', 'priority'],
    ['areas envolvidas', 'areas'],
    ['descricao detalhada', 'description'],
    ['prompt detalhado para a ia', 'aiPrompt'],
    ['funcionalidades esperadas', 'expectedFeatures'],
    ['contrato de auditoria', 'auditContract']
  ]);

  const VALID_TYPES = ['Ideia', 'Implementação', 'Melhoria', 'Ajuste', 'Bug', 'Correção'];
  const VALID_PRIORITIES = ['Baixa', 'Média', 'Alta', 'Crítica'];
  const MAX_ESTIMATED_TOKENS = 9000;
  const CONSERVATIVE_BYTES_PER_TOKEN = 2.6;

  const REQUIRED_FIELDS = [
    ['title', 'Título Direto'],
    ['summary', 'Resumo de Uma Frase'],
    ['description', 'Descrição Detalhada'],
    ['aiPrompt', 'Prompt Detalhado para a IA'],
    ['expectedFeatures', 'Funcionalidades Esperadas'],
    ['type', 'Tipo'],
    ['priority', 'Prioridade']
  ];

  function normalizeLabel(value) {
    return String(value || '')
      .normalize('NFD')
      .replace(/[\u0300-\u036f]/g, '')
      .toLowerCase()
      .replace(/^\s*(?:[-*+]\s+|#+\s+)?/, '')
      .replace(/[*_`]/g, '')
      .trim();
  }

  function parseHeading(line) {
    const match = String(line).match(/^\s*(?:[-*+]\s+)?(?:[*_`]+)?(.+?)(?:[*_`]+)?\s*:\s*(.*)$/);
    if (!match) return null;
    const field = FIELD_BY_LABEL.get(normalizeLabel(match[1]));
    return field ? { field, value: match[2] } : null;
  }

  function canonicalValue(value, allowedValues) {
    const normalized = normalizeLabel(value);
    return allowedValues.find((candidate) => normalizeLabel(candidate) === normalized) || '';
  }

  function estimateTokens(value) {
    const text = String(value || '');
    if (!text) return 0;
    const bytes = typeof TextEncoder === 'function' ? new TextEncoder().encode(text).length : text.length;
    const lexicalUnits = (text.match(/[\p{L}\p{N}_]+|[^\s\p{L}\p{N}_]/gu) || []).length;
    return Math.max(Math.ceil(bytes / CONSERVATIVE_BYTES_PER_TOKEN), lexicalUnits);
  }

  function classifySize(tokens) {
    if (tokens <= 4000) return 'compact';
    if (tokens <= 6000) return 'adequate';
    if (tokens <= MAX_ESTIMATED_TOKENS) return 'detailed';
    return 'needs further optimization';
  }

  function parse(text) {
    const source = String(text || '').replace(/\r\n?/g, '\n').trim();
    const values = {
      title: '', summary: '', type: 'Implementação', priority: 'Média', areas: '',
      description: '', aiPrompt: '', expectedFeatures: '', auditContract: ''
    };
    let activeField = null;
    const seenFields = new Set();

    source.split('\n').forEach((line) => {
      const heading = parseHeading(line);
      if (heading) {
        activeField = heading.field;
        seenFields.add(activeField);
        values[activeField] = heading.value.trim();
        return;
      }
      if (activeField) {
        values[activeField] += `${values[activeField] ? '\n' : ''}${line}`;
      }
    });

    Object.keys(values).forEach((key) => { values[key] = values[key].trim(); });
    const missing = REQUIRED_FIELDS
      .filter(([field]) => !values[field] || ((field === 'type' || field === 'priority') && !seenFields.has(field)))
      .map(([, label]) => label);
    const errors = missing.length ? [`Campos obrigatórios ausentes: ${missing.join(', ')}.`] : [];

    const type = canonicalValue(values.type, VALID_TYPES);
    const priority = canonicalValue(values.priority, VALID_PRIORITIES);
    if (seenFields.has('type') && values.type && !type) {
      errors.push(`Invalid Type. Use one of: ${VALID_TYPES.join(', ')}.`);
    }
    if (seenFields.has('priority') && values.priority && !priority) {
      errors.push(`Invalid Priority. Use one of: ${VALID_PRIORITIES.join(', ')}.`);
    }

    const metrics = {
      tokens: estimateTokens(source),
      words: (source.match(/\S+/g) || []).length,
      characters: [...source].length
    };
    metrics.classification = classifySize(metrics.tokens);
    if (metrics.tokens > MAX_ESTIMATED_TOKENS) {
      errors.push(`Complete Card exceeds the ${MAX_ESTIMATED_TOKENS.toLocaleString('en-US')}-token conservative estimated budget. Regenerate it without removing unique requirements.`);
    }

    return {
      valid: errors.length === 0,
      errors,
      metrics,
      card: {
        title: values.title,
        summary: values.summary,
        description: values.description,
        ai_prompt: values.aiPrompt,
        expected_features: values.expectedFeatures,
        audit_contract: values.auditContract,
        impacted_areas: values.areas.split(',').map((area) => area.trim()).filter(Boolean),
        type,
        priority,
        status: 'Pendente'
      }
    };
  }

  root.TaskBoardCardImporter = Object.freeze({ MAX_ESTIMATED_TOKENS, parse });
})(globalThis);
