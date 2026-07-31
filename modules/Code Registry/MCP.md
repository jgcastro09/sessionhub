# Code Registry MCP local

O Code Registry inclui um servidor MCP local, somente leitura. Ele roda por `stdio`: não abre porta de rede, não executa comandos solicitados pela IA e só devolve arquivos já registrados pelo Registry.

## Configuração

Use o executável Python local e o comando abaixo no cliente MCP compatível:

```text
C:\Python314\python.exe -B "D:\BUILD_LAB\BUNDLE_NODESTAGE\software_nodestage\Code Registry\tools\code_registry.py" mcp
```

No macOS, a forma equivalente é:

```text
python3 -B "/caminho/software_nodestage/Code Registry/tools/code_registry.py" mcp
```

O processo deve ser iniciado pelo cliente MCP; não execute o comando em um terminal compartilhado, pois a saída padrão é reservada exclusivamente para mensagens JSON-RPC.

## Ferramentas

- `search_code`: pesquisa híbrida local (FTS + embeddings) com aliases, ranking e razões de correspondência.
- `locate_ui_surface`: liga um conceito visível, como `build grid background`, aos candidatos de workspace, renderização e tema.
- `read_symbol`: devolve somente o corpo limitado de uma função ou método registrado, com linhas de origem.
- `get_file_context`: monta um pacote limitado por caracteres para um arquivo registrado.
- `get_module_context`: apresenta o mapa compacto de um módulo.
- `get_recent_changes`: lista mudanças registradas nos `Reload data`.
- `get_file_diff`: devolve o diff local mais recente de um arquivo.
- `get_git_audit`: mostra branch, upstream, divergência, alterações locais, conflitos, commits recentes e a correlação com os arquivos registrados; não executa fetch, pull, commit ou push.
- `get_git_file_history`: lista os commits Git que tocaram um arquivo registrado.
- `get_git_file_diff`: devolve o diff da árvore de trabalho contra `HEAD` ou de um commit contra seu pai.
- `registry_health`: informa a auditoria atual sem devolver código.

## Quando usar

O MCP é uma camada de descoberta, não uma substituição obrigatória para `rg` e leitura direta. Use-o para pedidos amplos, ambíguos, conceituais ou que cruzam módulos — por exemplo, “onde é composto o frame final?” ou “qual sistema controla conexões inválidas entre nodes?”.

Se a solicitação já contém um label visível, símbolo, caminho ou identificador exato — por exemplo, `License key`, `DrawTextInput` ou `LicensePanel.cpp` — a busca direta costuma ser mais rápida e deve ser preferida. Depois de localizar o dono, leia o código real em ambos os fluxos.

Após qualquer alteração, clique em **Reload data** no Explorer (ou execute `python "Code Registry/tools/code_registry.py" full`). A sincronização cria a linha do tempo local e atualiza a busca. O primeiro ciclo cria uma baseline; os próximos registram somente criações, modificações, remoções e movimentos detectados.

## Retrieval evaluation

Run the deterministic benchmark before claiming that the Registry improves retrieval:

```powershell
python "Code Registry/tools/evaluate_retrieval.py"
```

It compares the Registry against a direct lexical baseline using the maintained fixtures in `Code Registry/retrieval-evaluation.json`.

Semantic retrieval uses the local Ollama model configured in `config.json`. The first **Reload data** after enabling it builds the local index; later reloads embed only changed, new, moved, or removed registered files. If Ollama is off, FTS remains available and the MCP response says semantic retrieval was unavailable. No source code is sent to a cloud service.

## Limites e privacidade

- O histórico e os índices ficam em `Code Registry/data/history.sqlite3` e `Code Registry/data/semantic.sqlite3`; ambos podem ser reconstruídos.
- Arquivos excluídos, não registrados ou fora do repositório são recusados.
- Snapshots textuais têm limite de 512 KiB por arquivo; arquivos maiores preservam metadados e hash, sem conteúdo.
- Context Packs usam 16.000 caracteres por padrão e aceitam no máximo 60.000.
- O MCP não possui ferramentas de escrita, remoção, shell, rede ou acesso a segredos.
