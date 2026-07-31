# Plano: Arquitetura Project-First

Status: proposto, ainda não implementado.

## 1. Objetivo

Substituir `Session` como unidade principal do Session Hub por `Project`.
Um projeto passa a representar uma pasta de trabalho e tudo que é configurado
para ela: setup, tarefas, Code Registry, executors habilitados, automações,
filas, pipelines, watchers, métricas, checkpoints e contexto operacional.

Depois da migração, as únicas áreas globais da aplicação são:

1. **Projects**: catálogo de projetos conhecidos e seleção do projeto ativo.
2. **Executors**: catálogo local de CLIs/perfis instalados no computador.
3. **Settings**: preferências da aplicação, rede, Web Panel, atualizações,
   ferramentas instaladas e preços.

Todas as demais áreas são exibidas no escopo do projeto ativo. `Session` deixa
de existir como conceito de domínio e de interface; o tipo interno
`terminal.Session` continua existindo, pois é somente uma sessão de PTY e não
tem relação com um projeto.

## 2. Decisões arquiteturais

| Tema | Decisão |
| --- | --- |
| Unidade de trabalho | `Project`, identificado por UUID estável e raiz absoluta local. |
| Fonte canônica do projeto | `<raiz>/.shproject/`, com paths sempre relativos à raiz. |
| Dados globais | Catálogo de Executors, credenciais/perfis, Settings, ferramentas, rede e Web Panel. |
| Dados por projeto | Setup, bindings de Executor, tarefas, Registry e definições de automação. |
| Estado de execução | SQLite/local por `project_id`: instâncias PTY, locks, filas em andamento, idempotência, histórico, métricas, logs, checkpoints e caches. |
| Segredos | Nunca entram em `.shproject`; permanecem no Executor global ou no armazenamento local. |
| Interface global | Apenas Projects, Executors e Settings. |
| Interface do projeto | Overview, Terminals, Tasks, Registry, Setup, Project Executors, Queue, Pipelines, Automations, Metrics, Logs e Checkpoints. |
| Web Panel | Um único servidor do Session Hub, com rotas e SSE agrupados por projeto. |
| Compatibilidade | Não há camada legada: esta é uma substituição direta antes do primeiro lançamento. |

### 2.1 Executor global versus Executor do projeto

Um Executor instalado/configurado no computador continua global: comando,
perfil isolado, credenciais, variáveis secretas, método de instalação e dados
de preço pertencem ao usuário/máquina.

O projeto não duplica essa configuração. Ele armazena um **slot de Executor**
em `.shproject`, com nome, papel, regras de contexto e uma referência opcional
ao Executor local. Quando o projeto é aberto em outra máquina, o slot fica
`unmapped` até o usuário associá-lo a um Executor local compatível.

Isso permite, por exemplo, que `codex-implementer` seja uma exigência do
projeto sem versionar credenciais do Codex, caminhos de binário ou os MCPs
pessoais de quem abriu o repositório.

## 3. Contrato de `.shproject`

```text
<project-root>/
└── .shproject/
    ├── manifest.json
    ├── setup.json
    ├── executors.json
    ├── context/
    │   ├── brief.md
    │   ├── architecture.md
    │   ├── constraints.md
    │   └── decisions/
    ├── tasks/
    │   ├── workflow.json
    │   └── cards/
    ├── registry/
    │   ├── config.json
    │   └── records/
    └── automation/
        ├── definitions.json
        └── validation-recipes.json
```

### 3.1 `manifest.json`

O manifesto é pequeno, validável e sem segredos. Ele contém ao menos
`schema_version`, `project_id`, `name`, `root: "."`, data de criação e as
features habilitadas. O UUID não muda se a pasta for renomeada ou movida.

### 3.2 O que é versionado

Por padrão, todo o conteúdo acima é versionável, pois descreve a intenção do
projeto. O usuário pode optar por ignorar automações ou contextos que não
queira compartilhar, mas o padrão deve favorecer continuidade entre clones.

### 3.3 O que não é versionado

Os seguintes itens ficam em `~/.sessionhub/projects/<project_id>/` e nunca no
repositório:

- PTYs e instâncias em execução;
- terminal history e saída potencialmente sensível;
- locks, leases e chaves de idempotência;
- filas/retries em execução e recibos de efeitos;
- métricas e logs locais;
- snapshots/checkpoints operacionais;
- caches, índices derivados, embeddings e arquivos temporários;
- mapeamento local entre slots do projeto e Executors instalados.

## 4. Modelo de domínio alvo

```text
Global application
├── ExecutorCatalog
├── Settings
├── ProjectIndex
└── ProjectRuntimeStore

Project (.shproject)
├── ProjectSetup
├── ExecutorSlots
├── TaskManager
├── CodeRegistry
├── AutomationDefinitions
└── ProjectContext

Project runtime (SQLite local)
├── PTY instances
├── queue/pipeline executions
├── schedules and watcher cursors
├── locks and approvals
├── metrics, logs and checkpoints
└── context/cache revisions
```

### 4.1 Troca de identificadores

`domain.Session` deve ser substituído por `domain.Project`. Todos os campos
semânticos `SessionID` passam a ser `ProjectID`: instâncias, contexto,
checkpoints, métricas, queue items, schedules, pipelines, steps, approvals,
watchers, logs, reports, automações simples, APIs web e protocolo remoto.

Não renomear `internal/terminal.Session`, `terminal.Session` ou variáveis que
se referem estritamente a uma PTY; esses tipos representam outra coisa.

## 5. Experiência de interface

### 5.1 Navegação TUI

A navegação global passa a ter somente:

```text
Projects | Executors | Settings
```

Ao selecionar um projeto, a área principal apresenta navegação própria:

```text
Overview | Terminals | Tasks | Registry | Setup | Project Executors |
Queue | Pipelines | Automations | Metrics | Logs | Checkpoints
```

O topo sempre mostra versão, projeto ativo, raiz resumida, Executor/terminal
ativo e estado Git. Não existe mais aba ou formulário chamado “Session”.

### 5.2 Fluxos

- **Novo projeto**: escolher/criar pasta, inicializar `.shproject`, configurar
  setup inicial, selecionar slots de Executor e abrir o Overview.
- **Adicionar projeto existente**: selecionar raiz; se encontrar
  `.shproject`, validar e registrar; se não encontrar, inicializar o Project
  diretamente na raiz selecionada.
- **Abrir projeto**: selecionar um Project; os terminais usam a raiz dele e
  apenas seus slots habilitados são mostrados.
- **Remover projeto do catálogo**: remove somente o índice local; nunca toca
  nos arquivos da raiz. Excluir `.shproject` é uma ação distinta, confirmada.

### 5.3 Web Panel

O servidor HTTP já hospedado pelo Session Hub torna-se project-aware. A API
canônica passa a ser versionada sob `/api/v2`:

```text
GET  /api/v2/projects
GET  /api/v2/projects/{projectID}
GET  /api/v2/projects/{projectID}/executors
GET  /api/v2/projects/{projectID}/tasks
GET  /api/v2/projects/{projectID}/registry/search
GET  /api/v2/projects/{projectID}/queue
GET  /api/v2/projects/{projectID}/pipelines
GET  /api/v2/projects/{projectID}/automations
GET  /api/v2/projects/{projectID}/events
```

As rotas da SPA acompanham esse modelo (`/projects/:id`,
`/projects/:id/tasks`, `/projects/:id/registry`). SSE deve carregar
`project_id` em todos os eventos e o cliente descarta eventos de outro
projeto. A autenticação/pairing atual continua sendo do servidor global.

## 6. Persistência e schema direto

Esta mudança acontece antes do primeiro lançamento, portanto não há dados de
usuários, APIs públicas ou versões antigas a preservar. Não implementar
`session_id` e `project_id` em paralelo, adaptadores de leitura, wizard de
migração, fallback de workspace legado ou rotas HTTP duplicadas.

O schema SQLite deve ser refeito diretamente para `projects` e `project_id`.
Durante o desenvolvimento, apagar/recriar o banco local é o caminho aceito
quando uma migration de desenvolvimento não fizer sentido. Todos os
repositórios, fixtures e testes passam a usar somente `domain.Project`.

Filas e automações em execução não precisam de conversão. A nova arquitetura
nasce sem sessões persistidas e sem efeitos pendentes.

## 7. Serviços e pacotes

### 7.1 Novo `internal/project`

Responsabilidades:

- localizar `.shproject` a partir de uma pasta e validar containment;
- carregar/salvar manifesto e arquivos canônicos atomicamente;
- inicializar/adotar/remover do catálogo local;
- expor setup, executor slots e definições de automação;
- resolver o Project ativo para UI, Web Panel e automação;
- proteger contra path traversal, symlinks fora da raiz e schemas inválidos.

### 7.2 Serviços existentes

- `internal/executor`: recebe `projectID` e resolve o diretório de trabalho a
  partir da raiz do Project; nunca mais de um Session workspace.
- `internal/automation`: lê definições da `.shproject` e persiste apenas o
  estado de execução no SQLite local por Project.
- `internal/context`: renomeia o snapshot para Project Context e agrega task,
  registry, Git, instâncias e execução ativos.
- `internal/store`: armazena índice de Projects e runtime local; não vira
  fonte de verdade de setup, tasks, registry ou automações definidas no Git.
- `internal/webserver`: expõe contratos de Project, sem acessar o Store
  diretamente e sem oferecer leitura arbitrária de arquivos.
- `internal/remote`: troca frames `list_sessions`, `session_id` e
  `ViewState.SessionID` pelos equivalentes de Project. Como o protocolo já
  exige versões exatamente iguais, essa mudança pode ser intencionalmente
  incompatível entre releases.

## 8. Ordem de implementação

### Fase A — Contrato e fundação de Project

1. Criar `domain.Project`, manifestos e validação de `.shproject`.
2. Criar `internal/project` e catálogo local de Projects conhecidos.
3. Implementar init, attach, discovery por raiz e detach seguro.
4. Criar tabela/índice de Projects e o novo schema de runtime por Project.
5. Adicionar testes de schema, paths, projeto movido, projeto inválido e
   coexistência de dois projetos com nomes iguais.

Critério: é possível abrir, fechar e reabrir um Project sem criar Session.

### Fase B — Troca transversal de runtime

1. Mover dependências de `SessionID` para `ProjectID` no domínio e Store.
2. Migrar executor, context, automation, metrics, logs, checkpoints e locks.
3. Trocar protocol remoto e contratos Web Panel diretamente para Project.
4. Implementar recovery seguro para trabalho interrompido.
5. Remover qualquer fallback que inicie PTY sem Project selecionado.

Critério: cada operação de execução, auditoria e recovery é isolada por
Project e nenhum dado de um projeto é visível em outro.

### Fase C — Reorganização da TUI e Web Panel

1. Trocar “Sessions” por “Projects” no modelo, comandos, formulários,
   sidebar, topbar, documentos e testes.
2. Limitar navegação global a Projects, Executors e Settings.
3. Criar subnavegação de Project e migrar Queue, Pipelines, Automations,
   Metrics, Logs e Checkpoints para ela.
4. Atualizar SPA e API diretamente para `/api/v2/projects/{id}/...`.
5. Remover as rotas, textos e contratos de Session no mesmo change set.

Critério: trocar de Project troca toda a visão, os terminais disponíveis e os
dados operacionais sem reiniciar o Hub.

### Fase D — Executor slots e setup por projeto

1. Implementar `executors.json` com slots, papéis e políticas de contexto.
2. Criar UI para mapear um slot portável a um Executor global local.
3. Mover a associação hoje guardada em `Session.Settings.ExecutorIDs` para o
   manifesto do projeto.
4. Remover `WorkingDir` como decisão da Session; a raiz do Project é sempre o
   diretório de trabalho padrão.
5. Exibir slots ausentes/sem mapeamento de forma explícita e impedir o start
   até que sejam associados.

Critério: o mesmo repositório abre em outra máquina sem segredos e orienta o
usuário a mapear seus próprios Executors.

### Fase E — Capacidades de projeto

Somente após as fases anteriores, migrar os módulos planejados:

1. Task Board → Task Manager dentro de `.shproject/tasks`.
2. Code Registry genérico dentro de `.shproject/registry`.
3. Context Broker, incluindo tarefa ativa e Registry por Project.
4. Agent Bridge/MCP aditivo, configurado pelos slots de Executor do Project.
5. Interfaces web Task Manager e Registry Explorer servidas pelo Web Panel
   único do Session Hub.

Essa ordem evita construir Tasks/Registry sobre uma Session que deixará de
existir.

## 9. Regras de segurança e isolamento

- Toda operação resolve um Project antes de tocar dados ou iniciar uma PTY.
- Paths persistidos na `.shproject` são relativos e validados contra a raiz.
- Arquivos fora da raiz, symlinks de escape e paths absolutos são recusados.
- O Web Panel recebe um `projectID`, nunca um path arbitrário do navegador.
- Secrets, tokens, terminal output e MCPs pessoais não são serializados em
  arquivos versionados.
- Remover um Project da lista global não remove seu diretório.
- Deletar `.shproject`, automação, task ou Registry exige confirmação própria.
- A falha de Task Manager/Registry/Agent Bridge não impede abrir terminais
  normais do Project.

## 10. Testes e critérios de aceite

### Testes automatizados

- validação e round-trip de `.shproject`;
- descoberta do manifesto na raiz e em subpastas;
- isolamento de dados entre dois Projects;
- schema novo sem qualquer `session_id` de domínio;
- interrupção e recovery de automações por Project;
- mapeamento de slots sem vazar credenciais;
- start de PTY com raiz correta;
- APIs e SSE filtrados por Project;
- protocolo remoto com `project_id`;
- TUI: criação, seleção, troca e remoção do catálogo;
- regressões atuais de Store, automation, terminal e webserver adaptadas.

### Cenários manuais

1. Criar Project novo e iniciar dois Executors mapeados a ele.
2. Trocar para outro Project e confirmar que não há terminais, automações,
   logs ou métricas do primeiro na tela.
3. Abrir um projeto com slot `unmapped`, mapear ao Executor local e iniciar.
4. Abrir o Web Panel pareado em outro dispositivo e mudar de projeto sem ver
   dados cruzados.
5. Interromper o Hub com automação ativa e confirmar que a recuperação não
   repete efeitos concluídos.

### Verificação obrigatória de cada implementação

Após cada alteração de código desta iniciativa:

```sh
go build ./cmd/sessionhub
go test ./...
cd npm && npm test
```

Cada alteração também exige bump sincronizado de `VERSION`,
`cmd/sessionhub/main.go`, `npm/package.json` e entrada datada no
`CHANGELOG.md`, conforme `AGENTS.md`.

## 11. Fora de escopo desta migração

- Implementar o Task Manager e o Code Registry completos antes de Project.
- Qualquer compatibilidade, importação ou migração de Sessions antigas.
- Alterar ou sobrescrever skills, MCPs ou configurações globais de qualquer
  CLI do usuário.
- Sincronização em nuvem ou colaboração multiusuário.
- Remover o Web Panel atual; ele será migrado, não substituído por outro
  servidor.
- Transformar terminal PTY em “Project”; apenas o domínio anterior Session é
  renomeado/removido.

## 12. Resultado esperado

Ao fim das fases A–D, o Session Hub deixa de organizar o usuário por sessões
genéricas. Ele abre projetos concretos, cada um com sua própria configuração,
seus Executors habilitados e seu estado operacional isolado. Essa é a base
necessária para integrar Task Manager, Code Registry e o contexto automático
de agentes de forma coerente e portátil.
