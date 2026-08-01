# Plano: Code Registry global como serviço de contexto de projetos

Status: proposto. Sucessor evolutivo de `project-task-manager-code-registry.md`.

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

### Fase 0 — Corrigir a base de verdade

Objetivo: impedir saúde ou contexto incorreto.

1. Em `Health()`, comparar cada descoberta atual com a entrada armazenada:
   hash, tamanho, mtime, categoria, símbolos e dependências.
2. Derivar `PendingClassificationCount` do scan em memória, não somente de
   `pending.json` persistido.
3. Validar `Config.Roots`: paths relativos, sem `..`, dentro da raiz resolvida
   do projeto e sem sobreposição inesperada.
4. Incluir `LastFullScanAt`, resumo de scan e motivo de desatualização no
   relatório de saúde.
5. Cobrir alteração sem scan, novo pending sem scan, symlink e root inválido
   com testes de regressão.

Critério de aceite: nenhum arquivo elegível novo, removido ou alterado pode
deixar o relatório saudável até ser reconciliado ou explicitamente ignorado.

### Fase 1 — Scanner incremental real

Objetivo: atender projetos grandes sem analisar tudo em cada ação.

1. Criar cache derivado de fingerprint por caminho (`mtime`, tamanho e
   identificador do arquivo quando o SO o disponibilizar).
2. Reusar análise/hash quando o fingerprint é seguro; recalcular somente em
   arquivos novos ou potencialmente alterados.
3. Processar remoções pelo diff entre o inventário anterior e a caminhada
   atual; preservar `missing` e detectar rename por hash.
4. Executar full scan de auditoria sob comando, agendamento configurável ou
   quando o filesystem não fornecer fingerprint confiável.
5. Expor métricas: duração, arquivos vistos/reusados/reanalisados, bytes lidos
   e motivos de fallback.

Critério de aceite: rescan sem alteração não deve reler conteúdo nem regravar
registros; uma única alteração deve atualizar somente sua entrada e derivados.

### Fase 1.5 — Frescor contínuo e contrato de agentes

Objetivo: manter o contexto atualizado após alterações de pessoas, IDEs, Git e
agentes, sem assumir que todos os projetos usam o mesmo arquivo de instruções.

1. Adicionar watcher local por Project, opt-in e configurável, observando apenas
   roots elegíveis. Eventos `create`, `write`, `rename` e `remove` entram numa
   fila com debounce para evitar scan a cada salvamento intermediário.
2. O watcher executa somente o scanner incremental. Propostas de IA não rodam
   a cada evento: entram em fila após estabilização, por solicitação explícita
   ou de acordo com uma política de custo/privacidade escolhida pela pessoa.
3. Persistir estado de frescor (`fresh`, `updating`, `stale`, `failed`), último
   scan e motivo. Um full scan periódico ou manual continua sendo auditoria de
   integridade, não requisito para cada edição.
4. Criar `sessionhub registry ensure-fresh`. Ele reconcilia mudanças pendentes
   antes de entregar contexto e é chamado internamente por busca de contexto,
   impacto, Reader, auditoria e ferramentas MCP. Assim, com watcher desligado,
   o serviço também não entrega contexto silenciosamente velho.
5. Gerar um contrato separado, em
   `.shproject/registry/agent-contract.md`, que instrui agentes a assegurar
   frescor, pedir contexto antes de mudanças estruturais, consultar impacto e
   deixar o Registry reconciliar após alterações. Esse arquivo pertence ao
   Session Hub e nunca substitui instruções existentes do repositório.
6. Durante Setup, apenas detectar `AGENTS.md`, `CLAUDE.md`,
   `CONTRIBUTING.md` ou equivalentes e oferecer uma integração opcional. A UI
   deve mostrar um patch curto, delimitado por marcadores gerenciados, e só
   aplicá-lo após confirmação explícita. Nunca reescrever o arquivo, nem
   alterar qualquer texto fora dos marcadores.
7. Expor ferramentas MCP `registry_ensure_fresh`, `registry_search_context`,
   `registry_get_file_context`, `registry_impact_analysis`,
   `registry_propose_context` e `registry_confirm_context`. Todas as leituras
   de contexto asseguram frescor pela mesma camada de serviço.
8. Oferecer gate estrito somente por opt-in: `sessionhub registry health
   --strict` pode entrar em Audit Contract, hook ou CI, mas descrições pendentes
   não bloqueiam build/commit por padrão.

Critério de aceite: uma modificação local torna o Registry `stale` ou o
atualiza via watcher; antes de responder contexto, o serviço reconcilia o
estado. Nenhum arquivo de instruções do projeto é modificado sem preview e
consentimento explícitos.

### Fase 2 — Framework de analisadores por linguagem

Objetivo: aumentar fidelidade sem prender o serviço a uma linguagem.

1. Introduzir interface interna `Analyzer` para detectar linguagem, símbolos,
   imports, exports, includes e referências.
2. Preservar analisadores regex puros Go como fallback portátil.
3. Oferecer analisadores nativos para famílias mais comuns: Go, JavaScript/
   TypeScript, Python, C/C++, Rust, JVM, Shell, configuração e markup.
4. Definir adaptadores opcionais para parser/AST/LSP externos quando presentes;
   a ausência, erro ou versão desconhecida reduz confiança, não falha o scan.
5. Adicionar ao resultado `AnalyzerName`, `AnalyzerVersion` e confiança por
   fato extraído.

Critério de aceite: qualquer arquivo textual elegível entra no Registry; os
analisadores melhoram a precisão, mas nunca criam dependência obrigatória de
CGO, parser externo ou SDK de linguagem.

### Fase 3 — Contexto por IA com revisão humana completa

Objetivo: responder “o que é este arquivo?” em linguagem humana, com uma IA
propondo contexto com base em evidências, sem tirar da pessoa o controle sobre
o registro final.

1. Definir uma porta `ContextProposer` no núcleo. O Registry prepara um pacote
   limitado e estruturado do arquivo (path, linguagem, símbolos, imports,
   exports, trechos relevantes, relações e taxonomia) e a implementação da IA
   devolve uma proposta estruturada; o núcleo não depende de fornecedor ou
   modelo específico.
2. Fazer a IA propor, por arquivo: descrição curta, responsabilidades, tipo,
   módulo, áreas/tags, criticidade sugerida, arquivos relacionados e uma
   explicação de evidências. Cada sugestão deve declarar confiança e nunca
   afirmar que inferências são fatos.
3. Armazenar proposta, contexto confirmado e proveniência separadamente.
   A IA jamais sobrescreve texto manual; uma nova análise cria nova proposta
   ligada ao hash atual e preserva a anterior para auditoria.
4. Oferecer três fluxos manuais equivalentes:
   - **confirmar proposta**, opcionalmente com edição antes de confirmar;
   - **editar manualmente**, digitando descrição, responsabilidades, módulo,
     áreas, criticidade e relações sem chamar IA;
   - **classificar em lote**, para arquivos semelhantes, somente após preview
     explícito das entradas afetadas.
5. Criar fila de revisão priorizada por criticidade, mudança recente, alcance
   no grafo, confiança baixa, divergência entre IA e contexto manual e ausência
   de contexto confirmado.
6. Exibir no Reader uma interface clara: “confirmado por pessoa”, “proposta
   da IA”, “fatos detectados” e “evidências usadas”. Incluir ação de rejeitar
   uma sugestão e informar um motivo opcional para melhorar análises futuras.
7. Automatizar regras determinísticas que dão aparência inteligente sem
   inventar semântica: agrupamento por stem, pares de teste/implementação,
   resolução de import inequívoco, módulo por path, arquivos muito similares,
   impacto reverso e detecção de descrição desatualizada.

Critério de aceite: toda descrição entregue por API informa se é confirmada,
manual ou proposta pela IA, com hash/evidências de origem. Uma alteração de
conteúdo invalida a confirmação anterior e solicita uma nova proposta, mas
nunca apaga a escrita manual.

### Fase 4 — Busca e contexto de conteúdo

Objetivo: localizar implementação, não apenas metadados.

1. Estender FTS5 para indexar conteúdo textual limitado, símbolos e metadados,
   com trechos e linhas de ocorrência.
2. Separar campos privados/ignorados, binários e arquivos acima do limite;
   respeitar `.gitignore`, configuração e exclusões justificadas.
3. Gerar embeddings de uma representação limitada de conteúdo + símbolos +
   contexto humano, cacheados por hash/modelo.
4. Unificar ranking lexical e semântico com explicações de match e fallback
   determinístico se embeddings não estiverem disponíveis.
5. Expor `search_context`, `file_context`, `module_context`, `read_symbol` e
   `impact_analysis`, todos com orçamento de tamanho.

Critério de aceite: uma busca por termo presente apenas no código encontra o
arquivo, mostra trecho/linha e nunca exige que o arquivo tenha sido descrito
manualmente.

### Fase 5 — Arquitetura, relações e impacto

Objetivo: oferecer mapa técnico sem inventar conexões.

1. Generalizar taxonomia para módulos hierárquicos, áreas múltiplas, tags,
   tipos de relação e regras de logical unit configuráveis.
2. Modelar arestas `contains`, `imports`, `includes`, `implements`, `tests`,
   `generates`, `related` e `probable`, cada uma com origem e confiança.
3. Persistir diagnóstico de relações `unresolved`, `ambiguous` e `external`.
4. Criar grafo de impacto reverso com profundidade, limite e priorização por
   criticidade/revisão pendente.
5. Entregar visualização Web com filtros, subgrafo sob demanda e nenhuma
   renderização obrigatória do grafo total.

Critério de aceite: projetos com várias áreas podem mapear o mesmo arquivo em
vários contextos; relações não resolvidas continuam visíveis como incerteza.

### Fase 6 — Histórico e auditoria

Objetivo: tornar contexto resistente a mudanças locais e útil para revisão.

1. Criar SQLite derivado de histórico de scans, eventos e snapshots comprimidos
   com limite por arquivo e política de retenção.
2. Registrar `new`, `changed`, `renamed`, `missing`, `restored` e alterações de
   metadados/revisão.
3. Expor diff entre snapshots e, quando Git existir, histórico/diff por commit,
   estado de branch, upstream, conflitos e arquivos correlatos.
4. Git permanece somente leitura; qualquer operação de rede deve ser comando
   separado, explícito e jamais disparado pela UI de leitura.
5. Incluir histórico no contexto de arquivo com orçamento limitado.

Critério de aceite: uma mudança local não commitada pode ser explicada e
comparada depois de scan, sem depender de Git.

### Fase 7 — Serviço completo e superfícies

Objetivo: disponibilizar o contexto de forma consistente para pessoas e
ferramentas.

1. Consolidar API REST versionada para inventário, scan, health, busca,
   contexto, propostas de IA, confirmação/edição manual, grafo, histórico,
   revisão e configuração.
2. Acrescentar contrato MCP local/stdio sobre a mesma camada de aplicação;
   sem lógica paralela de filesystem ou índice.
3. Expandir CLI com saída JSON estável e comandos de contexto/diff/impacto.
4. Evoluir Web Panel com Overview, Reader, busca global, arquitetura, áreas,
   relações, histórico, auditoria Git e fila de revisão.
5. Manter TUI como resumo operacional e atalho seguro para fluxos avançados
   do painel, sem duplicar um grafo visual no terminal.

Critério de aceite: toda resposta nas três superfícies deriva do mesmo serviço
e tem contratos testados de autorização, paginação e orçamento de conteúdo.

### Fase 8 — Qualidade, escala e oferta como serviço

Objetivo: preparar uso por múltiplas pessoas e projetos reais.

1. Criar corpus de fixtures multi-linguagem, monorepo, symlinks, renames,
   arquivos grandes, binários, ambiguidades e permissões negadas.
2. Medir precisão de busca/contexto e benchmark de full/incremental scan.
3. Definir limites por projeto, cancelamento por contexto, progresso e erros
   parciais recuperáveis.
4. Documentar privacidade, retenção, modelos locais, backup de `.shproject` e
   migração de schema.
5. Implementar export/import auditável somente após estabilizar schema; nunca
   depender de serviço remoto para a operação local básica.

Critério de aceite: o produto opera offline em um projeto grande, informa
limites e incertezas e mantém dados canônicos recuperáveis após falhas de
índice ou atualização.

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

Cada fase exige: migração de dados compatível ou explícita, testes unitários e
de integração, benchmark proporcional ao risco, atualização do changelog e
uma revisão do comportamento em projeto real antes de iniciar a próxima.
