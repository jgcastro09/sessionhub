# Plano: Task Manager e Code Registry por Project

Status: implementado (v0.5.0). Pré-requisito: `project-first-architecture.md` concluído.

## 1. Objetivo

Adicionar Task Manager e Code Registry como capacidades nativas de cada
Project. Ambos usam a `.shproject` como fonte canônica e são apresentados pelo
Web Panel único do Session Hub, sem servidores Python, portas adicionais ou
aplicações independentes.

```text
Project
├── Setup
├── Executor slots
├── Task Manager
│   └── tarefas, aceite, validações e evidências
├── Code Registry
│   └── inventário, arquitetura, busca e revisão
└── Runtime local
    └── execução, cache, índices derivados e contexto ativo
```

Os módulos atuais em `modules/task-board` e `modules/Code Registry` servem
somente como referência de comportamento e formato. Não haverá compatibilidade
de runtime, importador, servidor legado ou dependência de Python.

## 2. Decisões de produto

| Tema | Decisão |
| --- | --- |
| Fonte de verdade de tarefas | Um Markdown por tarefa em `.shproject/tasks/cards/`. |
| Fonte de verdade do Registry | Registros semânticos JSON em `.shproject/registry/records/`. |
| Dados derivados | Inventários consolidados, auditorias, FTS e embeddings ficam no runtime local por Project. |
| UI principal | Páginas do Web Panel; TUI oferece resumo, navegação e ações rápidas. |
| Linguagem do núcleo | Go, integrado ao binário Session Hub. |
| Integração Task ↔ Registry | Tarefa referencia módulos, arquivos e `entry_id` estáveis do Registry. |
| Conclusão de tarefa | Manual por padrão; automática apenas quando o Audit Contract passa. |
| Busca semântica | Obrigatória no Code Registry. Embutida via llama.cpp, sem Ollama nem servidor externo; modelo `.gguf` baixado do Hugging Face no primeiro uso, mesmo padrão de instalação do Whisper em `internal/voice`. Busca lexical continua sempre disponível como caminho determinístico. |
| Superfícies | CLI, TUI e Web Panel consomem os mesmos serviços Go. CLI é a superfície scriptável (uso direto e automação); TUI cobre operação diária; Web Panel cobre os fluxos avançados. Ver seção 6. |

## 3. Persistência canônica

```text
.shproject/
├── tasks/
│   ├── workflow.json
│   └── cards/
│       └── TASK-0001.md
├── registry/
│   ├── config.json
│   └── records/
│       ├── go.json
│       ├── web.json
│       └── build.json
└── automation/
    └── validation-recipes.json
```

O runtime local em `~/.sessionhub/projects/<project_id>/` mantém apenas
artefatos reconstruíveis: índice FTS, cache de hashes, resultados de scan,
auditoria, histórico técnico e embeddings. Nenhuma saída de terminal,
credencial ou configuração pessoal de Executor é gravada na `.shproject`.

## 4. Task Manager

### 4.1 Card v1

Cada `TASK-XXXX.md` usa frontmatter simples e corpo Markdown. Campos mínimos:

```text
id, title, type, status, priority, created_at, updated_at,
impacted_areas, registry_refs, dependencies
```

O corpo contém seções delimitadas para:

- resumo e descrição detalhada;
- prompt/contexto sugerido para IA;
- critérios de aceite;
- referências técnicas;
- Audit Contract;
- Audit Report;
- evidências;
- resumo de conclusão;
- notas e histórico.

Os status internos são estáveis (`idea`, `backlog`, `ready`, `in_progress`,
`changes_requested`, `done`, `archived`). O Web Panel apresenta os nomes em
português e organiza o Kanban sem colocar texto localizado na regra de negócio.

### 4.2 Serviço `internal/tasks`

Responsabilidades:

- validar workflow e transições;
- ler, criar, atualizar e gravar cards atomicamente;
- gerar ID sequencial por Project com lock local;
- pesquisar por texto, status, prioridade, área e referência técnica;
- atualizar histórico e timestamps;
- resolver referências para o Registry;
- executar auditorias determinísticas;
- emitir eventos de Project para TUI, Web Panel e futura ponte de agentes.

Gravações usam arquivo temporário e rename atômico. O serviço recusa IDs
duplicados, frontmatter malformado e paths fora de `.shproject/tasks`.

### 4.3 Tarefa ativa e concorrência

O card contém o estado compartilhável da tarefa. A associação “qual Executor
ou terminal está trabalhando nela” pertence ao runtime local por Project, não
ao Git. Isso permite múltiplos Executors ativos sem um sobrescrever a tarefa
ativa do outro.

O Task Manager mostra claims ativos e avisa conflitos; ele não cria lock de
rede nesta fase.

### 4.4 Audit Contract

Uma tarefa pode declarar apenas verificações estruturadas:

```text
- registry: entry:<entry_id>
- source: internal/app/app.go contains NewProject
- validation: go-test
```

`validation` referencia receitas declaradas em
`.shproject/automation/validation-recipes.json`; comandos livres nunca são
executados a partir do card. O contrato deve incluir ao menos uma evidência de
Registry ou source e uma validação para poder concluir automaticamente.

## 5. Code Registry

### 5.1 Configuração genérica

`registry/config.json` define roots de scan, extensões, inclusões explícitas,
exclusões, categorias, limites e regras de cobertura. O inicializador de
Project propõe uma configuração baseada nos arquivos detectados, mas o
Registry não contém nomes, paths ou suposições do NodeStage.

Diretórios de dependências, build e `.shproject` são excluídos por padrão. O
usuário pode revisar essas regras no Setup do Project.

### 5.2 Entrada de Registry v1

Cada entrada possui:

- `entry_id` estável;
- path relativo atual;
- categoria, linguagem e módulo;
- descrição, responsabilidades e criticidade;
- relações confirmadas e relações prováveis;
- hash, tamanho, linhas e símbolos detectados;
- status e revisão semântica.

O scanner atualiza somente fatos automáticos. Descrição, módulo,
responsabilidades e relações revisadas são preservados. Um rename mantém o
`entry_id` quando a detecção por hash permitir; tarefas continuam apontando ao
mesmo item técnico.

### 5.3 Serviço `internal/registry`

Responsabilidades:

- descobrir arquivos elegíveis com containment rigoroso;
- analisar estrutura leve de Go, JS/TS, Python, HTML/CSS, shell e manifestos;
- calcular hashes e detectar mudanças/renames;
- sincronizar registros sem apagar revisão humana;
- gerar busca lexical, busca semântica e context packs limitados;
- validar cobertura, paths, hashes e revisão pendente — todo arquivo elegível
  do scan precisa ter uma entrada correspondente; cobertura incompleta,
  schema inválido ou hash sem revisão bloqueiam `validate`/`build`, nunca
  passam silenciosamente;
- correlacionar estado Git (branch, upstream, ahead/behind, mudanças no
  working tree, conflitos) com entradas do Registry, somente leitura, sem
  alterar worktree ou refs remotas;
- produzir auditoria e eventos;
- expor busca por arquivo, símbolo, módulo e conceito.

### 5.4 Busca semântica embutida

A busca semântica é obrigatória, não opcional. O `internal/registry` embute o
runtime de inferência do llama.cpp e baixa um modelo de embedding `.gguf` do
Hugging Face na primeira vez que o Registry é inicializado no Project — nunca
no install do CLI/npm. O download é verificado por SHA-256 fixo no código, com
progresso reportado, e cacheado em `~/.sessionhub/models/` para nunca repetir.
Esse é o mesmo padrão já usado por `internal/voice` para o Whisper
(`install.go`, `ensureModel`), reaproveitando `downloadVerified` e
`ProgressReporter`. Não há dependência de Ollama nem de servidor externo
rodando; o processo de embedding roda local e embutido no binário do
`sessionhub`. A busca lexical continua sempre disponível como caminho
determinístico, inclusive durante o download do modelo.

## 6. Interfaces CLI, TUI e Web

Três superfícies consomem os mesmos serviços Go (`internal/tasks`,
`internal/registry`); nenhuma delas acessa filesystem, Git ou modelo de
embedding diretamente — tudo passa pelos serviços.

### 6.1 CLI

O binário `sessionhub` ganha subcomandos scriptáveis, equivalentes ao que hoje
é feito com `python code_registry.py ...` e `python task_board.py ...`:

```text
sessionhub tasks list|create|show|status|search|audit
sessionhub registry scan|build|validate|search|context|review
```

A CLI é a superfície para uso direto no terminal e para automação (scripts,
hooks, chamadas por um agente via shell antes de existir uma ponte dedicada).
Ela não abre UI nem processo adicional; roda no processo do `sessionhub` e
sai. Saída em texto simples por padrão, com uma flag `--json` para consumo
programático.

### 6.2 TUI

A TUI cobre operação diária dentro do Project ativo, sem precisar abrir o
navegador:

- listar tarefas e criar novas;
- mudar status de uma tarefa;
- busca simples, lexical e semântica;
- rodar a auditoria de um card e ver o resultado;
- ver a saúde do Registry (cobertura, pendências, último scan);
- abrir e ler uma entrada do Registry.

Fluxos que exigem manipulação visual — Kanban com drag-and-drop, grafo de
arquitetura/relações, fila de revisão semântica, Reader de arquivo completo e
o editor do Audit Contract — ficam só no Web Panel; a TUI linka para a página
correspondente em vez de reimplementar essas visualizações em texto.

### 6.3 Web Panel

Novas rotas SPA:

```text
/projects/:projectID/tasks
/projects/:projectID/tasks/:taskID
/projects/:projectID/registry
/projects/:projectID/registry/:entryID
```

Novos endpoints autenticados:

```text
GET    /api/v2/projects/{id}/tasks
POST   /api/v2/projects/{id}/tasks
GET    /api/v2/projects/{id}/tasks/{taskID}
PATCH  /api/v2/projects/{id}/tasks/{taskID}
POST   /api/v2/projects/{id}/tasks/{taskID}/audit

GET    /api/v2/projects/{id}/registry/health
POST   /api/v2/projects/{id}/registry/scan
GET    /api/v2/projects/{id}/registry/search
GET    /api/v2/projects/{id}/registry/entries/{entryID}
GET    /api/v2/projects/{id}/registry/context
```

O navegador não recebe paths arbitrários e não lê o filesystem diretamente.
Todas as mutações passam pelos serviços Go e pelo pairing/autorização já
existente. SSE publica eventos com `project_id`, `kind`, `revision` e payload
limitado.

### 6.4 Task Manager web

- Kanban por status, filtros e pesquisa;
- criação/edição de card;
- página de detalhe com aceite, evidências e histórico;
- painel “Contexto técnico” com módulos e entradas relacionadas;
- auditoria por card e relatório legível;
- visualização dos claims runtime sem gravá-los no Git.

### 6.5 Registry Explorer web

- saúde e cobertura do inventário;
- busca por texto, módulo, linguagem, arquivo e símbolo;
- Reader de arquivo registrado e contexto limitado;
- fila de revisão semântica;
- relações e visão de arquitetura;
- ação explícita para scan/revisão, sem watcher web implícito.

## 7. Integração entre as duas capacidades

1. Ao criar ou editar uma tarefa, o usuário pesquisa o Registry e anexa
   referências estáveis (`entry_id`, módulo ou query salva).
2. A página da tarefa resolve essas referências e mostra paths atuais,
   responsabilidades, arquivos relacionados e revisão pendente.
3. Ao fazer scan, o Registry marca referências quebradas ou arquivos alterados
   desde a última evidência da tarefa.
4. A auditoria da tarefa executa o Audit Contract e grava o relatório no card.
5. Uma tarefa `done` que perde sua evidência em auditoria posterior passa para
   `changes_requested`.

Esta integração é determinística; ela não interpreta uma resposta de IA como
prova de implementação.

## 8. Ordem de implementação

### Fase A — Contratos e fundação

1. Definir schemas de `workflow.json`, card v1, config e record v1.
2. Criar `internal/tasks` e `internal/registry` com interfaces explícitas.
3. Adicionar paths da `.shproject` ao serviço Project.
4. Implementar atomic write, locks locais, events e validação de containment.
5. Criar fixtures de Project Go, Node, Python e misto.

Critério: serviços carregam e validam uma `.shproject` vazia sem UI.

### Fase B — Task Manager

1. Implementar CRUD, workflow, filtros, IDs e histórico.
2. Implementar Task API e eventos SSE.
3. Criar subcomandos `sessionhub tasks` na CLI (list/create/show/status/search/audit).
4. Criar Kanban e detalhe no Web Panel.
5. Adicionar na TUI: listar, criar, mudar status, busca simples e rodar auditoria de um card.
6. Implementar claims runtime e tarefa ativa por Executor/terminal.

Critério: duas janelas do Web Panel veem a mesma alteração de card sem
corromper Markdown ou perder histórico.

### Fase C — Registry

1. Implementar scanner, análise de arquivos, categorias e hashes.
2. Implementar records semânticos, review e validação com cobertura obrigatória
   (arquivo elegível sem entrada bloqueia `validate`/`build`).
3. Implementar correlação Git somente leitura (branch, upstream, mudanças,
   conflitos) associada a entradas do Registry.
4. Embutir llama.cpp e o download verificado do modelo de embedding via
   Hugging Face no primeiro uso do Registry, reaproveitando o padrão de
   instalação do Whisper em `internal/voice`; indexar busca semântica junto do
   índice lexical.
5. Criar subcomandos `sessionhub registry` na CLI (scan/build/validate/search/context/review).
6. Criar Registry API, página de saúde, busca, Reader e fila de revisão no
   Web Panel.
7. Adicionar na TUI: saúde do Registry, busca (lexical e semântica) e leitura
   de uma entrada.
8. Adicionar scan explícito no Setup e feedback por SSE.

Critério: um Project misto é indexado sem registrar dependências, artefatos de
build ou arquivos internos da `.shproject`, com busca semântica funcional
offline após o primeiro download do modelo.

### Fase D — Ligações e auditoria

1. Adicionar seletor de referências Registry no editor de tarefa.
2. Implementar resolução de `entry_id`, módulos e queries salvas.
3. Implementar receitas de validação e Audit Contract restrito.
4. Exibir impactos de mudanças do Registry em tarefas abertas/concluídas.
5. Atualizar contexto do Project com a tarefa e referências selecionadas.

Critério: uma tarefa só muda de status automaticamente por evidência
reproduzível, nunca por texto livre.

### Fase E — Qualidade e documentação

1. Atualizar arquitetura, uso, configuração e Web Panel docs.
2. Remover referências operacionais aos módulos independentes.
3. Exercitar recovery após scan/auditoria interrompidos.
4. Medir scan e busca em repositórios grandes; otimizar incrementalmente.

## 9. Testes e critérios de aceite

- parser/serializer de cards preserva conteúdo e gera diffs estáveis;
- transições inválidas e IDs duplicados são recusados;
- escrita concorrente preserva card válido;
- scanner não sai da raiz nem segue symlink inseguro;
- arquivo elegível sem entrada de Registry bloqueia `validate`/`build`, nunca
  passa silenciosamente;
- mudanças de hash exigem review semântico quando configurado;
- rename preserva referências estáveis quando reconhecido;
- correlação Git nunca altera worktree, index ou refs remotas;
- download do modelo de embedding verifica checksum e nunca bloqueia scan,
  validação ou busca lexical enquanto está em andamento ou falha;
- busca respeita limites de contexto e nunca devolve arquivo não registrado;
- validações rejeitam comandos não declarados;
- endpoints exigem pairing e não aceitam paths do cliente; a CLI local não
  precisa de pairing, mas herda as mesmas checagens de containment;
- SSE mantém isolamento estrito por Project;
- CLI, TUI e Web Panel produzem o mesmo resultado para a mesma ação e
  refletem eventos sem reiniciar o Hub;
- `go build ./cmd/sessionhub`, `go test ./...` e `npm test` passam a cada
  alteração de código.

## 10. Fora de escopo

- Compatibilidade com Task Board ou Code Registry em Python.
- Importação automática dos dados atuais.
- Ponte de agentes/MCP para Tasks ou Registry — fica para um plano futuro
  separado, quando necessário.
- Modificação de skills, MCPs ou configurações de CLI já existentes.
- Multiusuário e sincronização remota como requisito para o primeiro release.
- Execução de comandos livres a partir de cards.

## 11. Resultado esperado

Após este plano, abrir um Project no Session Hub entrega CLI, TUI e Web Panel
para organizar trabalho e localizar código: Task Manager mostra o que fazer e
como validar; Code Registry mostra onde atuar e qual o impacto, com busca
lexical e semântica local desde o primeiro uso. As três superfícies
compartilham a mesma `.shproject`, os mesmos serviços Go e a mesma identidade
de Project.
