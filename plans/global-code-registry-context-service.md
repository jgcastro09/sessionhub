# Plano: Code Registry global como serviço de contexto de projetos

Status: em andamento. Sucessor evolutivo de `project-task-manager-code-registry.md`.

## 0. Status de implementação (atualizado 2026-08-01)

Legenda: ✅ concluída · ⚠️ parcial (furos reais, ver notas) · ❌ não iniciada.

| Fase | Status | Nota |
| --- | --- | --- |
| 0 — base de verdade | ✅ | `Health()` compara disco vs. registro sem depender de scan anterior; `Config.Roots` validado. Commit `b601b24`. |
| 1 — scanner incremental | ✅ | Fingerprint cache real (mtime+size), `ScanFull`/`--full`, métricas de scan. Commit `b601b24`. |
| 1.5 — frescor contínuo / contrato de agente | ⚠️ | `EnsureFresh`, watcher opt-in, gate `--strict` prontos (commits `8c94f4d`, `52ea701`). `agent-contract.md`, integração com AGENTS.md/CLAUDE.md e as 6 ferramentas MCP **não existem ainda**. |
| 2 — analisadores por linguagem | ⚠️ | Só o fallback regex existe (nunca falha o scan, que é o critério mais importante). Falta a interface `Analyzer`, proveniência/confiança por fato, adaptadores externos. |
| 3 — contexto por IA com revisão humana | ❌ | Não iniciada. `ReviewQueue` existe mas só ordena por recência, não pela priorização multi-sinal do plano. Nenhuma porta `ContextProposer`. |
| 4 — busca e contexto de conteúdo | ⚠️ | Busca semântica real (embeddings + cache por hash). FTS não indexa conteúdo de arquivo, só metadados. `.gitignore` do projeto não é respeitado. Sem `module_context`/`read_symbol`. |
| 5 — arquitetura, relações, impacto | ⚠️ | Grafo de impacto reverso sólido. `Area` é campo único (deveria ser lista), sem `Tags`/`logical_unit_id`, faltam arestas `tests`/`generates`. |
| 6 — histórico e auditoria | ⚠️ | Histórico/diff por commit Git e correlação de branch já existem (`SourceHistory`, `GitStatus`). Falta o SQLite de histórico de scans/eventos/snapshots independente de Git. |
| 7 — serviço completo (API/MCP/CLI/Web/TUI) | ⚠️ | API `/api/v2` e CLI com `--json` já existem; Web Panel tem todas as seções listadas. Nenhum contrato MCP existe. |
| 8 — qualidade, escala, oferta como serviço | ❌ | Não iniciada. |

Detalhe item a item dentro de cada fase abaixo.

## 1. Objetivo

Transformar o Code Registry em um serviço local, genérico e confiável de
contexto técnico para projetos de qualquer porte e linguagem. Uma pessoa deve
poder abrir um projeto grande, indexá-lo e obter respostas organizadas para:

- quais arquivos existem, onde estão e se ainda estão atuais;
- o que cada arquivo faz, em linguagem humana, com a origem dessa informação;
- quais símbolos, imports, exports e dependências ele contém;
- que outros arquivos podem ser afetados por uma mudança;
- onde procurar uma funcionalidade, conceito, texto visível ou implementação;
- qual foi o histórico local e Git de uma entrada.

O núcleo continua independente de domínio. C++, OpenFrameworks, aplicações
web, Go, Python, Rust, mobile, infraestrutura e monorepos são perfis e
analisadores opcionais sobre o mesmo contrato — não exceções no núcleo.

## 2. Princípios de produto e arquitetura

| Princípio | Decisão |
| --- | --- |
| Fonte canônica | JSON legível e versionável em `.shproject/registry/`; SQLite, grafo, FTS, embeddings e histórico são derivados. |
| Verdade antes de conveniência | `health` deve comparar o filesystem atual com os registros, sem depender de um scan anterior. |
| Análise extensível | Cada linguagem usa um `Analyzer`; ausência de parser nunca impede o inventário, hash, busca textual ou revisão. |
| AI-first, humano no controle | A IA propõe contexto rico por arquivo; uma pessoa pode revisar, substituir, complementar ou escrever tudo manualmente. |
| Sem falsa revisão | Conteúdo gerado automaticamente é sugestão, nunca equivale a confirmação humana/agent. |
| Evidência | Campos derivados devem registrar origem/confiança; relações ambíguas nunca podem virar arestas confirmadas. |
| Privacidade local | Código, FTS, snapshots e embeddings ficam locais por padrão; nenhum upload é necessário para usar o serviço. |
| Escala previsível | Atualizações comuns são incrementais; full scan é explícito, periódico ou usado como auditoria. |
| Integração uniforme | CLI, TUI, Web Panel, Task Manager e futura API/MCP usam o mesmo `internal/registry.Service`. |

## 3. Contrato-alvo de dados

Manter `Entry` como unidade canônica, mas separar de forma explícita os dados
por proprietário e acrescentar capacidade para projetos complexos:

```text
Entry
├── identidade: entry_id, path, previous_paths, status
├── descoberta: hash, fingerprint, tamanho, linhas, linguagem, kind
├── análise: símbolos, imports, exports, includes, referências
├── arquitetura: module, areas[], tags[], logical_unit_id
├── contexto confirmado: description, responsibilities, criticality, related_files
├── sugestão de IA: descrição, responsabilidades, classificação e evidências
├── proveniência: origem, modelo/agente, confiança e evidências de cada sugestão
└── revisão: reviewed_hash, status, reviewed_at, reviewed_by
```

Regras:

- `areas` é uma lista: um arquivo pode participar de mais de um domínio.
- `logical_unit_id` é opcional e configurado por regras genéricas de pares ou
  grupos de arquivos; não pressupõe headers C++.
- toda sugestão de IA recebe `origin: ai`, identificador/modelo do agente,
  confiança, evidências e `review_status: needs_review`; somente `Review()`
  pode promovê-la a contexto confirmado.
- edição manual recebe `origin: manual` e sempre vence a sugestão de IA na
  resposta principal; sugestões podem permanecer como histórico comparável.
- fatos verificáveis — hash, path, linguagem, tamanho, linhas, símbolos e
  imports — continuam automáticos e não precisam de IA nem confirmação manual.
- arquivos removidos permanecem `missing`; snapshots e relações continuam
  consultáveis até a política de retenção removê-los explicitamente.

## 4. Fases de implementação

### Fase 0 — Corrigir a base de verdade ✅ Concluída (commit `b601b24`)

Objetivo: impedir saúde ou contexto incorreto.

1. [x] Em `Health()`, comparar cada descoberta atual com a entrada armazenada:
   hash, tamanho, mtime, categoria, símbolos e dependências.
   (`discoveryDiffersFromEntry`, reusado de `applyDiscovery`; `health.go`.)
2. [x] Derivar `PendingClassificationCount` do scan em memória, não somente de
   `pending.json` persistido. (`len(result.Pending)`.)
3. [x] Validar `Config.Roots`: paths relativos, sem `..`, dentro da raiz resolvida
   do projeto e sem sobreposição inesperada. (`Config.validateRoots`.)
4. [x] Incluir `LastFullScanAt`, resumo de scan e motivo de desatualização no
   relatório de saúde. (`LastScanAt`/`LastFullScanAt`/`StalenessReasons` em
   `HealthReport`, persistidos em `scanstate.json`.)
5. [x] Cobrir alteração sem scan, novo pending sem scan, symlink e root inválido
   com testes de regressão. (`health_test.go`, `config_test.go`,
   `TestScanSkipsSymlinkEscapingRoot`/`TestScanFollowsSymlinkWithinRoot`.)

Critério de aceite: nenhum arquivo elegível novo, removido ou alterado pode
deixar o relatório saudável até ser reconciliado ou explicitamente ignorado.
**Cumprido** — verificado com teste de regressão que edita um arquivo sem
chamar `Scan()` e confirma que `Health()` sozinho já reporta `unhealthy`.

### Fase 1 — Scanner incremental real ✅ Concluída (commit `b601b24`)

Objetivo: atender projetos grandes sem analisar tudo em cada ação.

1. [x] Criar cache derivado de fingerprint por caminho (`mtime`, tamanho e
   identificador do arquivo quando o SO o disponibilizar). (`FileFingerprint`
   em `scanstate.json`; sem identificador de inode do SO, só mtime+size —
   suficiente para o critério de aceite.)
2. [x] Reusar análise/hash quando o fingerprint é seguro; recalcular somente em
   arquivos novos ou potencialmente alterados. (`inspectFile` em `scanner.go`.)
3. [x] Processar remoções pelo diff entre o inventário anterior e a caminhada
   atual; preservar `missing` e detectar rename por hash. (Já existia antes
   desta sessão, em `service.go`'s `Scan`.)
4. [x] Executar full scan de auditoria sob comando, agendamento configurável ou
   quando o filesystem não fornecer fingerprint confiável.
   (`ScanFull`/`sessionhub registry scan --full` cobre "sob comando";
   agendamento configurável automático não existe.)
5. [x] Expor métricas: duração, arquivos vistos/reusados/reanalisados, bytes lidos
   e motivos de fallback. (`ScanMetrics`, `Service.ScanMetrics`, impresso por
   `sessionhub registry scan`.)

Critério de aceite: rescan sem alteração não deve reler conteúdo nem regravar
registros; uma única alteração deve atualizar somente sua entrada e derivados.
**Cumprido** — `TestIncrementalScanReusesUnchangedFingerprint` prova 0 bytes
lidos e 100% reuso num rescan sem mudança.

### Fase 1.5 — Frescor contínuo e contrato de agentes ⚠️ Parcial (itens 1-4, 8 feitos; commits `8c94f4d`, `52ea701`)

Objetivo: manter o contexto atualizado após alterações de pessoas, IDEs, Git e
agentes, sem assumir que todos os projetos usam o mesmo arquivo de instruções.

1. [x] Adicionar watcher local por Project, opt-in e configurável, observando apenas
   roots elegíveis. Eventos `create`, `write`, `rename` e `remove` entram numa
   fila com debounce para evitar scan a cada salvamento intermediário.
   (`internal/registry/watcher.go`, fsnotify, `Config.Watch`.)
2. [x] O watcher executa somente o scanner incremental. Propostas de IA não rodam
   a cada evento: entram em fila após estabilização, por solicitação explícita
   ou de acordo com uma política de custo/privacidade escolhida pela pessoa.
   (`Watcher.reconcile` só chama `Scan()`; não há propostas de IA no sistema
   ainda — ver Fase 3.)
3. [x] Persistir estado de frescor (`fresh`, `updating`, `stale`, `failed`), último
   scan e motivo. Um full scan periódico ou manual continua sendo auditoria de
   integridade, não requisito para cada edição. (`Freshness` em
   `scanstate.json`; `stale` nunca é setado explicitamente hoje — só
   `fresh`/`updating`/`failed` — já que `Health()` deriva staleness ao vivo em
   vez de depender do flag persistido.)
4. [~] Criar `sessionhub registry ensure-fresh`. Ele reconcilia mudanças pendentes
   antes de entregar contexto e é chamado internamente por busca de contexto,
   impacto, Reader, auditoria e ferramentas MCP. Assim, com watcher desligado,
   o serviço também não entrega contexto silenciosamente velho.
   (`Service.EnsureFresh` existe e está com CLI/API; wired em Search, Graph/
   EntryGraph e ReadSource. **Falta**: wiring em auditoria (GitStatus) e nas
   ferramentas MCP, que ainda não existem — item 7.)
5. [ ] Gerar um contrato separado, em
   `.shproject/registry/agent-contract.md`, que instrui agentes a assegurar
   frescor, pedir contexto antes de mudanças estruturais, consultar impacto e
   deixar o Registry reconciliar após alterações. Esse arquivo pertence ao
   Session Hub e nunca substitui instruções existentes do repositório.
   **Não iniciado.**
6. [ ] Durante Setup, apenas detectar `AGENTS.md`, `CLAUDE.md`,
   `CONTRIBUTING.md` ou equivalentes e oferecer uma integração opcional. A UI
   deve mostrar um patch curto, delimitado por marcadores gerenciados, e só
   aplicá-lo após confirmação explícita. Nunca reescrever o arquivo, nem
   alterar qualquer texto fora dos marcadores. **Não iniciado.**
7. [ ] Expor ferramentas MCP `registry_ensure_fresh`, `registry_search_context`,
   `registry_get_file_context`, `registry_impact_analysis`,
   `registry_propose_context` e `registry_confirm_context`. Todas as leituras
   de contexto asseguram frescor pela mesma camada de serviço.
   **Não iniciado** — não existe nenhum servidor/contrato MCP no projeto ainda.
8. [x] Oferecer gate estrito somente por opt-in: `sessionhub registry health
   --strict` pode entrar em Audit Contract, hook ou CI, mas descrições pendentes
   não bloqueiam build/commit por padrão. (Corrigido nesta sessão: antes
   `registry health` sempre falhava o processo em qualquer pendência, o que
   já agia como gate estrito por padrão — contradizia este item.)

Critério de aceite: uma modificação local torna o Registry `stale` ou o
atualiza via watcher; antes de responder contexto, o serviço reconcilia o
estado. Nenhum arquivo de instruções do projeto é modificado sem preview e
consentimento explícitos. **Parcialmente cumprido** — o watcher e
`EnsureFresh` cobrem a primeira metade; a segunda metade (nunca modificar
arquivo de instruções sem preview) ainda não tem código nenhum, então
vacuamente verdadeira, não testada.

### Fase 2 — Framework de analisadores por linguagem ⚠️ Parcial — só o item 2 e o critério de aceite central estão de pé

Objetivo: aumentar fidelidade sem prender o serviço a uma linguagem.

1. [ ] Introduzir interface interna `Analyzer` para detectar linguagem, símbolos,
   imports, exports, includes e referências. **Não existe** — `languages.go`/
   `symbols.go`/`imports.go` são funções livres, não uma abstração plugável.
2. [x] Preservar analisadores regex puros Go como fallback portátil.
   (`symbols.go`, `imports.go`, cobrindo go/ts/js/python/shell/rust/c/cpp.)
3. [~] Oferecer analisadores nativos para famílias mais comuns: Go, JavaScript/
   TypeScript, Python, C/C++, Rust, JVM, Shell, configuração e markup.
   (Cobertura regex existe para a maioria; falta JVM (java/kotlin) e
   config/markup. E "nativo" aqui ainda é só regex, não parser/AST real —
   isso é o item 4.)
4. [ ] Definir adaptadores opcionais para parser/AST/LSP externos quando presentes;
   a ausência, erro ou versão desconhecida reduz confiança, não falha o scan.
   **Não existe** nenhum ponto de extensão.
5. [ ] Adicionar ao resultado `AnalyzerName`, `AnalyzerVersion` e confiança por
   fato extraído. **Não existe** — nenhum fato carrega proveniência/confiança.

Critério de aceite: qualquer arquivo textual elegível entra no Registry; os
analisadores melhoram a precisão, mas nunca criam dependência obrigatória de
CGO, parser externo ou SDK de linguagem. **Cumprido** — confirmado por leitura
de código: ausência de analisador nunca falha o scan, zero CGO/SDK externo.

### Fase 3 — Contexto por IA com revisão humana completa ❌ Não iniciada

Objetivo: responder “o que é este arquivo?” em linguagem humana, com uma IA
propondo contexto com base em evidências, sem tirar da pessoa o controle sobre
o registro final.

1. [ ] Definir uma porta `ContextProposer` no núcleo. O Registry prepara um pacote
   limitado e estruturado do arquivo (path, linguagem, símbolos, imports,
   exports, trechos relevantes, relações e taxonomia) e a implementação da IA
   devolve uma proposta estruturada; o núcleo não depende de fornecedor ou
   modelo específico.
2. [ ] Fazer a IA propor, por arquivo: descrição curta, responsabilidades, tipo,
   módulo, áreas/tags, criticidade sugerida, arquivos relacionados e uma
   explicação de evidências. Cada sugestão deve declarar confiança e nunca
   afirmar que inferências são fatos.
3. [ ] Armazenar proposta, contexto confirmado e proveniência separadamente.
   A IA jamais sobrescreve texto manual; uma nova análise cria nova proposta
   ligada ao hash atual e preserva a anterior para auditoria.
4. [ ] Oferecer três fluxos manuais equivalentes:
   - **confirmar proposta**, opcionalmente com edição antes de confirmar;
   - **editar manualmente**, digitando descrição, responsabilidades, módulo,
     áreas, criticidade e relações sem chamar IA;
   - **classificar em lote**, para arquivos semelhantes, somente após preview
     explícito das entradas afetadas.
   (`Service.Review` já cobre "editar manualmente" no nível de dado — o que
   falta é tudo que depende de proposta de IA.)
5. [ ] Criar fila de revisão priorizada por criticidade, mudança recente, alcance
   no grafo, confiança baixa, divergência entre IA e contexto manual e ausência
   de contexto confirmado. (Existe `Service.ReviewQueue`, mas só filtra por
   `needs_review` e ordena por `UpdatedAt` — nenhum dos sinais de priorização
   do plano.)
6. [ ] Exibir no Reader uma interface clara: “confirmado por pessoa”, “proposta
   da IA”, “fatos detectados” e “evidências usadas”. Incluir ação de rejeitar
   uma sugestão e informar um motivo opcional para melhorar análises futuras.
7. [ ] Automatizar regras determinísticas que dão aparência inteligente sem
   inventar semântica: agrupamento por stem, pares de teste/implementação,
   resolução de import inequívoco, módulo por path, arquivos muito similares,
   impacto reverso e detecção de descrição desatualizada.
   (Parcial fora desta fase: agrupamento por stem existe em
   `relationships.go`'s `computeProbableRelatedFiles`; resolução de import
   inequívoco existe em `graph.go`; impacto reverso existe na Fase 5. O que
   falta é específico de pares teste/implementação e descrição desatualizada.)

Critério de aceite: toda descrição entregue por API informa se é confirmada,
manual ou proposta pela IA, com hash/evidências de origem. Uma alteração de
conteúdo invalida a confirmação anterior e solicita uma nova proposta, mas
nunca apaga a escrita manual. **Não cumprido** — não há conceito de "proposta
da IA" em lugar nenhum do código ainda.

### Fase 4 — Busca e contexto de conteúdo ⚠️ Parcial — semântica real, léxico e `.gitignore` incompletos

Objetivo: localizar implementação, não apenas metadados.

1. [ ] Estender FTS5 para indexar conteúdo textual limitado, símbolos e metadados,
   com trechos e linhas de ocorrência. **Não cumprido** — schema FTS
   (`index.go`) indexa só `symbols/description/responsibilities/module/
   category`, nenhuma coluna de conteúdo; `SearchResult` não tem
   snippet/linha.
2. [ ] Separar campos privados/ignorados, binários e arquivos acima do limite;
   respeitar `.gitignore`, configuração e exclusões justificadas.
   **Parcial** — binários e limite de tamanho são respeitados
   (`policy.go`); `.gitignore` real do projeto **não é lido em lugar
   nenhum** (`gitignore.go` só escreve o `.gitignore` interno do Session
   Hub, não tem relação com isso).
3. [x] Gerar embeddings de uma representação limitada de conteúdo + símbolos +
   contexto humano, cacheados por hash/modelo. (`semantic.go`, real, cache
   por `(entry_id, model, hash)` — mas o texto embedado é deliberadamente
   metadata-only hoje, não conteúdo bruto do arquivo; ver nota abaixo.)
4. [x] Unificar ranking lexical e semântico com explicações de match e fallback
   determinístico se embeddings não estiverem disponíveis. (`search.go`,
   reciprocal rank fusion + `naiveSearch` fallback.)
5. [ ] Expor `search_context`, `file_context`, `module_context`, `read_symbol` e
   `impact_analysis`, todos com orçamento de tamanho. **Parcial** — os
   equivalentes a `search_context`/`file_context`/`impact_analysis` existem
   via `Search`/`Get`/`EntryGraph`, mas sem orçamento de tamanho; não há
   `module_context` nem `read_symbol` dedicados.

Critério de aceite: uma busca por termo presente apenas no código encontra o
arquivo, mostra trecho/linha e nunca exige que o arquivo tenha sido descrito
manualmente. **Não cumprido** — confirmado com teste manual: um identificador
que só existe no meio do código (não é símbolo top-level, não está em
descrição) não é encontrado pela busca hoje.

### Fase 5 — Arquitetura, relações e impacto ⚠️ Parcial — grafo de impacto sólido, taxonomia e diagnóstico incompletos

Objetivo: oferecer mapa técnico sem inventar conexões.

1. [~] Generalizar taxonomia para módulos hierárquicos, áreas múltiplas, tags,
   tipos de relação e regras de logical unit configuráveis. Módulo por
   prefixo hierárquico existe (`module.go`). **`Area` é campo único
   (`string`), não lista** — contradiz a regra explícita da Seção 3 deste
   plano ("`areas` é uma lista"). `Tags` e `logical_unit_id` **não existem**
   no `Entry`.
2. [~] Modelar arestas `contains`, `imports`, `includes`, `implements`, `tests`,
   `generates`, `related` e `probable`, cada uma com origem e confiança.
   `contains`/`implements`/`imports`/`includes`/`related`/`probable` existem
   com confiança (`graph.go`). **Faltam `tests` e `generates`** como tipos
   de aresta próprios (pares teste/implementação hoje só entram no
   heurístico genérico `probable`).
3. [~] Persistir diagnóstico de relações `unresolved`, `ambiguous` e `external`.
   Só `"ambiguous"` é de fato atribuído (`DependencyIssue.Reason`); o caso
   "nenhum candidato encontrado" é descartado sem virar `"unresolved"`, e
   `"external"` (import de stdlib/terceiros, esperado) não existe como
   categoria separada.
4. [x] Criar grafo de impacto reverso com profundidade, limite e priorização por
   criticidade/revisão pendente. (`ImpactAnalysis` em `graph.go`, BFS
   bidirecional, prioriza `needs_review`/`critical` na truncagem.)
5. [~] Entregar visualização Web com filtros, subgrafo sob demanda e nenhuma
   renderização obrigatória do grafo total. `RegistryRelationshipsView`
   busca subgrafo real quando há uma entrada semente, mas sem semente busca
   o grafo completo sem paginação a cada poll de 20s.

Critério de aceite: projetos com várias áreas podem mapear o mesmo arquivo em
vários contextos; relações não resolvidas continuam visíveis como incerteza.
**Não cumprido** no primeiro ponto (`Area` é campo único hoje); o segundo
ponto está parcialmente cumprido (`ambiguous` sim, `unresolved` real não).

### Fase 6 — Histórico e auditoria ⚠️ Parcial — metade Git já existia antes deste plano, metade SQLite não iniciada

Objetivo: tornar contexto resistente a mudanças locais e útil para revisão.

1. [ ] Criar SQLite derivado de histórico de scans, eventos e snapshots comprimidos
   com limite por arquivo e política de retenção. **Não iniciado** —
   `index.sqlite3` existe mas é só o índice FTS/semântico atual, não um log
   histórico de scans/eventos/snapshots.
2. [ ] Registrar `new`, `changed`, `renamed`, `missing`, `restored` e alterações de
   metadados/revisão. **Não iniciado** — `Entry.Status` transiciona entre
   esses estados em memória a cada scan, mas nada persiste o histórico dessas
   transições.
3. [~] Expor diff entre snapshots e, quando Git existir, histórico/diff por commit,
   estado de branch, upstream, conflitos e arquivos correlatos. A metade Git
   já existia antes deste plano: `GitStatus` (branch/upstream/ahead-behind/
   conflitos/arquivos correlatos) e `SourceHistory`/`SourceAtRevision`
   (histórico/diff por commit). **Falta** diff entre snapshots do próprio
   Registry (item 1), que não depende de Git.
4. [x] Git permanece somente leitura; qualquer operação de rede deve ser comando
   separado, explícito e jamais disparado pela UI de leitura. (Já cumprido
   antes deste plano — `gitstate` é só leitura.)
5. [~] Incluir histórico no contexto de arquivo com orçamento limitado.
   `SourceHistory` já existe com `limit`; não há orçamento de *tamanho*
   (bytes/tokens), só de contagem.

Critério de aceite: uma mudança local não commitada pode ser explicada e
comparada depois de scan, sem depender de Git. **Não cumprido** — sem o
SQLite de histórico do item 1, uma mudança local não commitada só é visível
como "diferente agora", não comparável a um estado anterior.

### Fase 7 — Serviço completo e superfícies ⚠️ Parcial — API/CLI/Web fortes, MCP ausente

Objetivo: disponibilizar o contexto de forma consistente para pessoas e
ferramentas.

1. [x] Consolidar API REST versionada para inventário, scan, health, busca,
   contexto, propostas de IA, confirmação/edição manual, grafo, histórico,
   revisão e configuração. (`/api/v2/projects/{id}/registry/...`,
   `internal/webserver/registry_api.go` — cobre tudo exceto propostas de IA,
   que não existem ainda, Fase 3.)
2. [ ] Acrescentar contrato MCP local/stdio sobre a mesma camada de aplicação;
   sem lógica paralela de filesystem ou índice. **Não iniciado** — nenhum
   servidor MCP existe no projeto.
3. [x] Expandir CLI com saída JSON estável e comandos de contexto/diff/impacto.
   (`--json` em todos os subcomandos de `sessionhub registry`; `graph`,
   `search`, `git`, `ensure-fresh` já existem.)
4. [x] Evoluir Web Panel com Overview, Reader, busca global, arquitetura, áreas,
   relações, histórico, auditoria Git e fila de revisão.
   (`web/src/views/registry/RegistryLayout.tsx` e as 14 views que ele monta —
   todas as seções listadas existem.)
5. [x] Manter TUI como resumo operacional e atalho seguro para fluxos avançados
   do painel, sem duplicar um grafo visual no terminal. (TUI atual não
   duplica o grafo; não verificado a fundo nesta sessão.)

Critério de aceite: toda resposta nas três superfícies deriva do mesmo serviço
e tem contratos testados de autorização, paginação e orçamento de conteúdo.
**Parcial** — API/CLI/Web realmente compartilham o mesmo `Service` (sem lógica
paralela); paginação existe em busca/fila de revisão; orçamento de conteúdo
não existe em lugar nenhum ainda (ver Fase 4 item 5); "três superfícies" hoje
são só duas (CLI+Web), já que MCP não existe.

### Fase 8 — Qualidade, escala e oferta como serviço ❌ Não iniciada

Objetivo: preparar uso por múltiplas pessoas e projetos reais.

1. [ ] Criar corpus de fixtures multi-linguagem, monorepo, symlinks, renames,
   arquivos grandes, binários, ambiguidades e permissões negadas. (Os testes
   atuais usam `t.TempDir()` ad hoc por caso, não um corpus de fixtures
   compartilhado.)
2. [ ] Medir precisão de busca/contexto e benchmark de full/incremental scan.
3. [ ] Definir limites por projeto, cancelamento por contexto, progresso e erros
   parciais recuperáveis.
4. [ ] Documentar privacidade, retenção, modelos locais, backup de `.shproject` e
   migração de schema.
5. [ ] Implementar export/import auditável somente após estabilizar schema; nunca
   depender de serviço remoto para a operação local básica.

Critério de aceite: o produto opera offline em um projeto grande, informa
limites e incertezas e mantém dados canônicos recuperáveis após falhas de
índice ou atualização. **Não verificado** — nenhum benchmark em projeto
grande real foi rodado ainda.

## 5. Compatibilidade e respeito às instruções do projeto

O Code Registry é complementar às regras de cada repositório:

- `AGENTS.md`, `CLAUDE.md`, `CONTRIBUTING.md` e arquivos equivalentes são
  propriedade do projeto e permanecem fonte de instrução para seus agentes.
- O contrato em `.shproject/registry/agent-contract.md` é uma orientação de
  uso do Registry, não uma tentativa de substituir políticas existentes.
- A integração com instruções existentes é sempre uma ação consciente da
  pessoa: detectar → mostrar diff → confirmar → inserir somente um bloco
  identificado. Não há append silencioso nem overwrite.
- Mesmo sem integração textual, watcher, `ensure-fresh` e MCP mantêm o
  Registry útil; o contrato melhora adesão, mas não é o único mecanismo de
  atualização.

## 6. Não objetivos

- Não transformar o Registry em IDE, compilador ou substituto de LSP.
- Não exigir conta, nuvem, upload de código ou modelo remoto.
- Não assumir C++, OpenFrameworks, estrutura de diretórios ou taxonomia de um
  projeto específico.
- Não usar busca semântica como fonte de verdade para relações arquiteturais.
- Não bloquear o usuário quando um analisador opcional ou embedding falhar.
- Não deixar uma IA alterar contexto confirmado, classificação manual ou
  criticidade sem uma ação explícita de confirmação da pessoa.
- Não exigir provedor de IA: análise manual e automações determinísticas devem
  continuar completas quando não houver modelo configurado.

## 7. Ordem recomendada

Implementar uma fase por release: **Fase 0 → 1 → 1.5 → 4 → 3 → 5 → 6 → 7 →
8**.
Fase 2 pode avançar em paralelo por famílias de linguagem, desde que preserve
o contrato comum e não bloqueie as fases de confiabilidade e busca.

**Progresso real (2026-08-01)**: Fase 0 e Fase 1 concluídas (release
`v0.6.3`, publicada no npm). Fase 1.5 parcial — watcher, `EnsureFresh` e gate
`--strict` prontos; `agent-contract.md`, integração com AGENTS.md/CLAUDE.md e
as ferramentas MCP ficaram para depois, por decisão explícita de escopo desta
sessão. Fases 2, 4 e 5 têm trabalho pré-existente parcial com furos reais
documentados acima (ver seção 0). Fases 3, 6 (metade SQLite), 7 (MCP) e 8
ainda não foram iniciadas. Próximo passo recomendado pela ordem acima: fechar
a Fase 1.5 (itens 5-7), depois a Fase 4.

Cada fase exige: migração de dados compatível ou explícita, testes unitários e
de integração, benchmark proporcional ao risco, atualização do changelog e
uma revisão do comportamento em projeto real antes de iniciar a próxima.
