# Plano: Encadeamento de Automações (cadeia linear com contexto estruturado)

Status: proposto, ainda não implementado.
Escopo: sistema `Scheduler`/`SimpleAutomation` (`internal/automation`). O sistema `Engine`/`Pipeline` (SQLite) não é tocado.

## 1. Contexto e motivação

Hoje uma `SimpleAutomation` (`internal/automation/simple.go`) roda sua lista de `Steps` em sequência e para — nada é disparado depois que ela termina. Não existe conceito de "quando esta automação acabar, inicie outra".

A versão 0.3.26 já resolveu a parte mais frágil para viabilizar isso: cada step, ao ser enviado à CLI, ganha em runtime — sem tocar no prompt salvo pelo usuário — um aviso de que é uma automação, uma instrução para agir de forma autônoma, e a exigência de emitir um marcador fixo (`SESSIONHUB_AUTOMATION_COMPLETE`) somente quando o trabalho estiver de fato concluído:

- Envelope do prompt: `internal/executor/service.go:404-414` (`automationPrompt`).
- Constante do gatilho: `internal/executor/service.go:62` (`AutomationCompletionToken`).
- Reconhecimento do gatilho: `internal/executor/service.go:544-562` (`handleOutput`), via `strings.Contains` no buffer acumulado da execução.
- Fallback antigo (5s de silêncio sem novo output) continua existindo e intacto em `internal/executor/service.go:592-604` (`checkWork`), só entra em ação quando o token não aparece.

Isso já dá um sinal de conclusão bem mais confiável que a heurística de silêncio pura. É a base sobre a qual este plano constrói.

**Objetivo deste plano**: quando o usuário configurar, uma automação, ao concluir **com sucesso confirmado**, dispara automaticamente a próxima automação da cadeia — podendo estar na mesma aba/sessão ou em outra CLI/sessão — passando adiante um **resumo estruturado** (gerado pela própria IA) do que foi feito, como contexto extra para a automação seguinte. Sem alterar o prompt salvo de nenhuma automação.

## 2. Decisões de formato (já validadas com o usuário)

| Decisão | Escolha |
|---|---|
| Formato do encadeamento | Cadeia linear (A → B → C), sem ping-pong nem auto-repetição de uma mesma automação |
| Critério de parada | Teto de profundidade da cadeia (rede de segurança) **+** exigência de sinal de conclusão explícito da IA (não heurística) |
| Conteúdo passado adiante | Resumo estruturado escrito pela própria IA antes do gatilho de conclusão — não a saída bruta do terminal |

### Regra central pedida pelo usuário: "regras pra IA não ficar viajando"

Traduzido em comportamento concreto:
1. Só encadear em **sucesso** — nunca em falha, cancelamento, ou timeout.
2. Só encadear quando o **último step** da automação terminou porque a IA emitiu explicitamente o token de conclusão — nunca quando terminou pelo fallback de silêncio de 5s ou por outra regra genérica. (Hoje `Status == StatusCompleted` pode vir de qualquer um desses caminhos indistintamente — isso muda, ver seção 3.)
3. Teto rígido de elos por cadeia (proposto: 5), para qualquer cadeia mal configurada em ciclo parar sozinha em vez de rodar para sempre.
4. Uma automação não pode apontar para si mesma (bloqueado já no formulário e na validação).

## 3. Mudanças por arquivo

### 3.1 `internal/automation/simple.go`

**Modelo de dados**
- `SimpleAutomation`: novo campo `NextAutomationID string \`json:"nextAutomationId,omitempty"\`` — vazio significa "sem encadeamento" (100% compatível com automações já salvas, que não terão esse campo no JSON).
- `LastRun`: novo campo `ChainedTo string \`json:"chainedTo,omitempty"\`` — guarda o nome/ID da automação disparada em seguida, ou uma nota curta ("chain depth limit reached (5)") quando o teto impediu o disparo. Aparece no histórico da automação na TUI.
- Nova constante: `const maxChainDepth = 5`.

**`Scheduler.start`** (hoje `start(automationID, trigger string, occurrence time.Time)`, `simple.go:252-282`)
- Assinatura estendida: `start(automationID, trigger string, occurrence time.Time, chainDepth int, chainContext string)`.
- Os dois chamadores existentes passam a mandar valores neutros:
  - `RunNow` (`simple.go:217-219`) → `s.start(automationID, "manual", time.Now(), 0, "")`.
  - `process` (`simple.go:247-248`) → `s.start(item.id, "scheduled", item.at, 0, "")`.
- Resto da função (dedup por `s.seen`, checagem de "já rodando", criação do `context.WithCancel`) fica igual; só passa `chainDepth`/`chainContext` adiante para `s.execute`.

**`Scheduler.execute`** (`simple.go:284-380`)
- Assinatura estendida: `execute(ctx context.Context, automationID string, chainDepth int, chainContext string)`.
- Antes do `for i, step := range item.Steps`, declarar `var lastResult executor.WorkResult` — hoje `result`/`err` só existem dentro do loop interno `for attempt := 1; ; attempt++` (escopo local a cada iteração), e precisam sobreviver até depois do loop para decidir o encadeamento.
- No **primeiro step** (`i == 0`) com `chainContext != ""`: montar o prompt efetivamente enviado como
  ```
  "Context from the previous automation:\n" + chainContext + "\n\n---\n\n" + step.Prompt
  ```
  e passar esse texto (não `step.Prompt` puro) para `RunAutomationStepWithProgress`. O `step.Prompt` armazenado no JSON nunca muda — o prefixo é só para aquela chamada de runtime, mesmo padrão já usado pelo envelope de automação da 0.3.26.
- No **último step** (`i == len(item.Steps)-1`) de uma automação com `item.NextAutomationID != ""`: chamar `RunAutomationStepWithProgress` com o novo parâmetro `requestSummary: true` (ver 3.2), pedindo à IA o bloco de resumo antes do token de conclusão. Nos demais steps, `requestSummary: false`.
- Quando `err == nil` dentro do loop de retry: além do preview já existente (`previews = append(...)`), gravar `lastResult = result`.
- No bloco final, onde `current.Status` é decidido (`simple.go:343-379`): depois de calcular `StatusCompleted`/`StatusFailed`/`StatusCanceled` como hoje, adicionar a lógica de encadeamento:
  ```
  se current.Status == StatusCompleted && current.NextAutomationID != "":
      se lastResult.RuleID != "automation_completion_token":
          # terminou "de verdade" segundo o sistema, mas não por confirmação explícita da IA —
          # não é confiável o suficiente pra encadear. Não faz nada, cadeia não avança.
      senão se chainDepth+1 > maxChainDepth:
          current.LastRun.ChainedTo = "chain depth limit reached (5), not triggering"
      senão:
          summary = extractSummary(lastResult)   # ver função nova abaixo
          # guardar current.NextAutomationID e summary em variáveis locais;
          # o disparo em si só acontece DEPOIS de soltar s.mu (ver abaixo)
  ```
- **Importante**: `s.start(...)` faz `s.mu.Lock()` internamente. `execute()` não pode chamar `s.start` enquanto ainda segura o mutex do próprio `Scheduler` (`sync.RWMutex` não é reentrante — mesmo cuidado que já existe documentado em `internal/executor/service.go:132-137` para o event loop do executor). Portanto: calcular tudo o que for necessário para o encadeamento *dentro* da seção crítica (`s.mu.Lock()`/`s.mu.Unlock()` já existente no fim de `execute`), mas só chamar `s.start(current.NextAutomationID, "chained", time.Now(), chainDepth+1, summary)` **depois** do `s.mu.Unlock()`.
- Se `s.start(...)` retornar erro (próxima automação foi apagada, ou já está rodando por outro motivo): não é fatal para a automação atual, que já terminou normalmente. Só logar via `s.store.Log(...)` (mesmo padrão de `internal/executor/service.go:163-166`).

**Nova função `extractSummary`**
```go
// extractSummary lê o bloco delimitado pelos marcadores de resumo do
// resultado do último step. Retorna "" se a IA não os emitiu — o
// encadeamento segue adiante sem contexto extra, nunca falha por isso.
func extractSummary(result executor.WorkResult) string {
    text := SanitizeTerminalOutput(result.Screen)
    if text == "" {
        text = SanitizeTerminalOutput(result.Output)
    }
    start := strings.Index(text, executor.AutomationSummaryStartToken)
    end := strings.Index(text, executor.AutomationSummaryEndToken)
    if start == -1 || end == -1 || end < start {
        return ""
    }
    return strings.TrimSpace(text[start+len(executor.AutomationSummaryStartToken) : end])
}
```

**`SanitizeTerminalOutput`** (`simple.go:423-440`)
- Remover também os dois novos marcadores de resumo do texto exibido no histórico, no mesmo ponto onde hoje já remove `executor.AutomationCompletionToken` (linha 426):
  ```go
  value = strings.ReplaceAll(value, executor.AutomationSummaryStartToken, "")
  value = strings.ReplaceAll(value, executor.AutomationSummaryEndToken, "")
  ```

**`validateSimple`** (`simple.go:584-603`)
- Adicionar checagem: se `item.NextAutomationID != "" && item.NextAutomationID == item.ID`, retornar erro `"an automation cannot chain to itself"`. Bloqueia o ciclo mais óbvio (1 elo) já no salvamento; o teto de profundidade em runtime cobre ciclos maiores (A→B→A→B...).

### 3.2 `internal/executor/service.go`

**Novas constantes**, ao lado de `AutomationCompletionToken` (linha 62):
```go
const AutomationSummaryStartToken = "SESSIONHUB_SUMMARY_START"
const AutomationSummaryEndToken = "SESSIONHUB_SUMMARY_END"
```

**`WorkResult`** (linhas 67-76): adicionar campo `RuleID string` — qual regra de reconhecimento decidiu a conclusão daquele step (`"automation_completion_token"`, `"automation_idle"`, `"process_exit"`, ou o ID de uma `RecognitionRule` configurada). É esse campo que permite ao Scheduler diferenciar "terminou porque a IA confirmou" de "terminou porque o sistema assumiu por heurística".

**`finishWork`** (linha 608+): ao montar o `WorkResult` enviado por `work.done` (linha 634), incluir `RuleID: recognition.RuleID`.

**`automationPrompt`** (linhas 404-414): adicionar parâmetro `requestSummary bool`. Quando `true`, inserir — antes da instrução da linha final de conclusão — algo como:
```
Before that final line, write a short summary block for the next automation in the chain:
one line with exactly `SESSIONHUB_SUMMARY_START`, then a concise bullet list (10 lines max)
of what was done, key decisions, and files touched, then one line with exactly
`SESSIONHUB_SUMMARY_END`.
```
O limite de "10 linhas" é deliberado: o texto que chega de volta ao Scheduler é limitado a uma cauda de ~1200-1800 bytes (`outputPreview`/`automationOutputPreview`, contrato já existente e intencional — `WorkResult` é "bounded completion evidence", comentário em `service.go:64-66`), então um resumo longo correria risco real de ser cortado antes do encadeamento conseguir lê-lo.

**`RunAutomationStepWithProgress`** (linha ~290): adicionar parâmetro `requestSummary bool`, repassado para `automationPrompt`. Único chamador (`Scheduler.execute`) atualizado conforme 3.1.

### 3.3 `internal/ui/model.go`

**`editAutomationForm` / `automationFromEditor`** (`model.go:2833-2856`, `2940-2990`)
- Nova lista de escolha `form.automationNextChoices []automationChoice`, mesmo padrão de `automationSessions`/`automationExecutors`, populada a partir de `m.app.AutomationScheduler.List()` com uma opção extra `"(none)"` no topo e **excluindo a própria automação sendo editada** (reforço da proteção contra auto-referência já na UI, antes de chegar em `validateSimple`).
- `automationFromEditor` grava o ID escolhido em `NextAutomationID` (`""` se `"(none)"` estiver selecionado).
- `newAutomationForm` (criação nova, sem `item` ainda): lista igual, mas nada pré-selecionado além de `"(none)"`.

**`automationDetailsForm`** (`model.go:2867-2898`)
- Mostrar uma linha `Chained to: <valor>` quando `item.LastRun.ChainedTo != ""`, no mesmo `strings.Builder` já usado ali (perto da linha 2886-2890, junto com `Failed step:`/`Error:`).

## 4. O que fica de fora, deliberadamente

- **Sem `OnFailure`** nesta primeira versão — falha nunca propaga a cadeia, só sucesso confirmado pelo token explícito. Pode ser adicionado depois como campo separado (`NextOnFailureAutomationID`) sem quebrar nada do que está aqui.
- **Sem grafo de dependências** — uma automação aponta para no máximo **uma** próxima. Não é pipeline com paralelismo/junção; é cadeia simples.
- **Sem token de "parar a cadeia"** dado pela IA em runtime — continuar ou não a cadeia é 100% definido na configuração (`NextAutomationID` presente ou vazio), decidido pelo usuário ao montar as automações, não pela IA em tempo de execução.
- **Sistema Engine/Pipeline (SQLite) não é tocado** — o encadeamento vive só no Scheduler/SimpleAutomation, mesmo lugar onde a 0.3.26 já investiu (aviso de automação, gatilho de conclusão).

## 5. Compatibilidade e risco

- Campos novos são `omitempty` — automações já salvas em `automations.json` continuam carregando normalmente, com `NextAutomationID == ""` (comportamento idêntico ao atual).
- Nenhuma mudança de schema em disco além de campos adicionais na mesma struct — sem migração.
- Pior caso de mau uso (cadeia circular mal configurada): para sozinha no teto de profundidade, registra o motivo no histórico, não trava o `Scheduler` nem consome recursos indefinidamente.
- Pior caso de resumo ausente/malformado: `extractSummary` retorna `""`, a próxima automação roda normalmente sem contexto extra — nunca é motivo de falha.

## 6. Verificação

1. Criar automação A (1 step, prompt simples) com `NextAutomationID` apontando para automação B (1 step), mesma sessão e mesmo executor (mesma aba). Rodar A com "Run Now" e confirmar que B inicia sozinha ao final, com o prompt enviado a B contendo o bloco de contexto extraído de A (visível no live output/histórico).
2. Repetir com B usando um Executor diferente (outra aba/CLI) na mesma sessão — confirmar que o encadeamento cruza CLIs normalmente.
3. Tentar salvar uma automação com `NextAutomationID` igual ao próprio ID — confirmar rejeição no formulário e em `validateSimple`.
4. Forçar uma cadeia circular (A→B→A) editando os IDs diretamente e rodar — confirmar que para sozinha na profundidade 5, sem travar, e que `LastRun.ChainedTo` registra o motivo.
5. Fazer um step de A falhar (ex.: executor inexistente) — confirmar que B **não** é disparada.
6. Automação sem `RecognitionRule` configurada, que conclui só pelo fallback de silêncio de 5s (não pelo token) — confirmar que, mesmo com `Status == StatusCompleted`, a próxima automação **não** é disparada (porque `lastResult.RuleID != "automation_completion_token"`).
7. `go build ./...` e `go test ./internal/automation/... ./internal/executor/...` continuam passando.
