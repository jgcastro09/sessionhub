const assert = require('assert');
const fs = require('fs');
const vm = require('vm');

const context = { globalThis: {}, TextEncoder };
vm.createContext(context);
vm.runInContext(fs.readFileSync('task-board/app/js/card-importer.js', 'utf8'), context);

const imported = context.globalThis.TaskBoardCardImporter.parse(`* Título Direto: Implementar Media Manager e Collect Project

* Resumo de Uma Frase: Criar um gerenciador central de arquivos do projeto.

* Tipo: Implementação

* Prioridade: Crítica

* Áreas Envolvidas: Project System, Asset Management, Hot Backup

* Descrição Detalhada:

Manter Asset ID estável e preservar o projeto original.

* Prompt Detalhado para a IA:

# Tarefa

Implementar Media Manager.

## Regras obrigatórias

* Não sobrescrever arquivos diferentes.

* Funcionalidades Esperadas:

* Importação por texto completo.
* Persistência sem campos adicionais.`);

assert.ok(imported.valid, imported.errors.join('\n'));
assert.strictEqual(imported.card.title, 'Implementar Media Manager e Collect Project');
assert.strictEqual(imported.card.priority, 'Crítica');
assert.deepStrictEqual(Array.from(imported.card.impacted_areas), ['Project System', 'Asset Management', 'Hot Backup']);
assert.ok(imported.card.ai_prompt.includes('## Regras obrigatórias'));
assert.ok(imported.card.expected_features.includes('Persistência sem campos adicionais.'));
assert.strictEqual(context.globalThis.TaskBoardCardImporter.parse('* Título Direto: Incompleto').valid, false);

const invalidType = context.globalThis.TaskBoardCardImporter.parse(`* Título Direto: Test
* Resumo de Uma Frase: Test
* Tipo: Feature
* Prioridade: Alta
* Descrição Detalhada: Test
* Prompt Detalhado para a IA: Test
* Funcionalidades Esperadas: Test`);
assert.ok(invalidType.errors.some((error) => error.includes('Invalid Type')));

const oversized = context.globalThis.TaskBoardCardImporter.parse(`* Título Direto: Test
* Resumo de Uma Frase: Test
* Tipo: Implementação
* Prioridade: Alta
* Descrição Detalhada: Test
* Prompt Detalhado para a IA: ${'technical requirement '.repeat(12000)}
* Funcionalidades Esperadas: Test`);
assert.ok(oversized.errors.some((error) => error.includes('9,000-token')));

const oversizedCompleteCard = context.globalThis.TaskBoardCardImporter.parse(`* Título Direto: Test
* Resumo de Uma Frase: Test
* Tipo: Implementação
* Prioridade: Alta
* Descrição Detalhada: ${'unique technical requirement '.repeat(2000)}
* Prompt Detalhado para a IA: Concise execution instructions.
* Funcionalidades Esperadas: Test`);
assert.ok(oversizedCompleteCard.errors.some((error) => error.includes('Complete Card exceeds')));
console.log('card-importer.test.js: OK');
