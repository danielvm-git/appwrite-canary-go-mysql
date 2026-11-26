# Arquitetura

## Fluxo

```
              Appwrite (appwrite.danielvm.net)
  ┌───────────────────────────────────────────────────────────┐
  │  Auth (sessão)        Storage (attachments)               │
  │     ▲                       ▲                             │
  │     │                       │                             │
  │  Site estático (web/) ──────┘  (harness; shell UI depois) │
  │     │                                                     │
  │     └── execute ──▶ Function todo-api-mysql (Go)          │
  └───────────────────────────────┬───────────────────────────┘
                                  │ TLS 1.3 + CA do endpoint
                                  ▼
                     MySQL HeatWave 10.0.1.126:3306 (privado)
                     schema todo_mysql
```

## Decisões

1. **A Function é a API.** O HeatWave é privado por construção (subnet
   `10.0.1.0/24`, Security List só libera 3306 de `10.0.0.0/24`). O browser nunca
   fala SQL; a Function autentica via `x-appwrite-user-id` e fala com o banco.
2. **Um `http.Handler`, dois entrypoints.** `cmd/api` (local, `net/http`) e
   `main.go` (Function, contrato Open Runtimes) compartilham `internal/api`.
   O runtime Go do Appwrite não é um servidor HTTP: ele chama `Main(ctx)`.
3. **Permissões no servidor.** Appwrite guarda `sharedWith` como array na linha e
   deixa o PATCH cru tocar outras colunas. Aqui a colaboração é uma tabela e o
   handler limita o colaborador a `status`.
4. **Schema embutido + rota autenticada.** `internal/schema/sql/*.sql` entra no
   binário; `POST /admin/migrate` exige `MIGRATE_TOKEN`. É assim que o CI aplica
   migration sem SSH e sem cliente MySQL.
5. **TLS com CA pinada.** O certificado do endpoint tem só `CN=MySQL_Endpoint_Server`
   (sem SAN), então a verificação de hostname do Go nunca passaria. Verificamos a
   cadeia contra a CA do endpoint e dispensamos apenas o hostname
   (`internal/db/db.go`).
6. **Deploy só via CI.** Igual aos canários irmãos: `workflow_run` → deploy da
   Function → migrate → smoke. Nenhum artefato é copiado para a VPS.
