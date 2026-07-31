# NodeStage Task Board

O **Task Board** é a fonte oficial de tarefas, pendências, ideias, melhorias, ajustes, correções e bugs do projeto NodeStage. Ele é um Kanban local simples, funcional, gravável e diretamente consultável pelo sistema de arquivos.

---

## 1. Como Iniciar a Interface Web

No Windows, execute de duas formas:
- Clique duplo em `Start-Task-Board.bat` na pasta `task-board/`.
- Ou via terminal:
  ```bash
  python task-board/scripts/task_board.py serve
  ```

No macOS / Linux:
- Execute `Start-Task-Board.command` ou `python task-board/scripts/task_board.py serve`.

A interface abrirá automaticamente no navegador em `http://localhost:8090/`.

---

## 2. Onde os Cards Ficam Armazenados

Cada card é armazenado como um arquivo Markdown individual com ID estável na pasta:
```text
task-board/data/cards/
├── TASK-0001.md
├── TASK-0002.md
└── ...
```

Não há banco de dados centralizado nem servidores externos. O frontmatter Markdown contém os metadados do Kanban; o corpo contém os requisitos técnicos com quebras de linha reais. Cada `.md` é a única fonte de verdade do Card.

---

## 3. Tipos e Status Disponíveis

### Tipos de Card
- `Ideia`: sugestões de novas funcionalidades ou conceitos.
- `Implementação`: tarefas de desenvolvimento de novas partes do código.
- `Melhoria`: aprimoramentos de funcionalidades já existentes.
- `Ajuste`: modificações pontuais ou refatorações.
- `Bug`: falhas de comportamento ou crashes.
- `Correção`: resolução de erros e pequenos defeitos.

### Status das Colunas
1. `Ideias`: banco de ideias inicial.
2. `Pendente`: tarefas prontas para implementação.
3. `Em andamento`: tarefa em execução ativa.
4. `Ajuste necessário`: funcionalidades implementadas que necessitam de correção ou refinamento.
5. `Implementado`: (recolhido por padrão) tarefa concluída e validada.
6. `Arquivado`: (recolhido por padrão) cards aposentados ou obsoletos.

---

## 4. Como uma IA deve Consultar e Atualizar os Cards

As IAs podem interagir via arquivos Markdown diretamente em `task-board/data/cards/` ou usando a CLI utilitária. A CLI e a API preservam o mesmo formato Markdown; `--json` produz apenas uma visão temporária de saída, sem criar arquivos JSON.

### Listar cards:
```bash
python task-board/scripts/task_board.py list [--status STATUS] [--type TYPE] [--priority PRIORITY] [--json]
```

### Pesquisar cards por texto:
```bash
python task-board/scripts/task_board.py search "VMProtect"
```

### Exibir detalhes de um card:
```bash
python task-board/scripts/task_board.py show TASK-0001
```

### Alterar status do card:
```bash
# Para iniciar:
python task-board/scripts/task_board.py status TASK-0001 "Em andamento"

# Para concluir e registrar solução:
python task-board/scripts/task_board.py complete TASK-0001 --summary "Integrado VMProtect no build Release."

# Para retornar para ajuste:
python task-board/scripts/task_board.py reopen TASK-0001 --reason "Falhou no build x64."
```

### Criar novo card:
```bash
python task-board/scripts/task_board.py create --title "Título" --type Bug --priority Alta --summary "Resumo"
```

---

## 5. Auditoria determinística com Code Registry

Um Card pode conter a seção `Audit Contract` no próprio arquivo Markdown. Ela é opcional, mas é a única base para mudança automática de status: texto livre, título, áreas envolvidas e respostas de IA nunca são tratados como prova de implementação.

Cada linha aceita uma verificação restrita:

```text
- registry: app/src/nodes/NodeRegistry.cpp
- source: app/src/nodes/NodeRegistry.cpp contains Compositor
- validation: code-registry-validate
```

- `registry` confirma que um arquivo mantido é localizável no Code Registry.
- `source` confirma um literal em um arquivo relativo dentro do repositório.
- `validation` executa somente validações locais permitidas: `code-registry-validate`, `change-contract`, `task-board-markdown-storage` ou `task-board-card-importer`. Comandos livres não são aceitos.

O contrato precisa conter ao menos uma evidência `registry` ou `source` e uma `validation`. Execute a auditoria por Card ou para todos:

```bash
python task-board/scripts/task_board.py audit TASK-0007
python task-board/scripts/task_board.py audit
```

Quando todas as verificações passam, o Card vai para `Implementado`; uma falha coloca Card não concluído em `Pendente` e Card já concluído em `Ajuste necessário`. Sem contrato, o Card permanece no status manual e recebe relatório `NOT CONFIGURED`.

---

## 6. Regras para IAs

1. A IA deve consultar o Task Board antes de iniciar qualquer implementação relevante.
2. Alterar o status para `Em andamento` ao iniciar o trabalho num card.
3. Marcar como `Implementado` somente após código escrito E verificação/validação executada.
4. Registrar o resumo da solução e a data de conclusão ao marcar como implementado.
5. Em caso de ajuste de um card já implementado, mover para `Ajuste necessário` mantendo o histórico original.
