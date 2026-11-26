# appwrite-canary-go-mysql

O produto Todo do canário Go — **mesmos requisitos**, com os dados em
**Oracle MySQL HeatWave** em vez do TablesDB do Appwrite.

- Requisitos de origem: `../specs/product/TODO_APP_REQUIREMENTS.md` (workspace
  `appwrite-canaries`) e as specs deste repo em `specs/`.
- Appwrite: projeto **`todo-mysql`** em https://appwrite.danielvm.net
- Função: `todo-api-mysql` (runtime `go-1.26`, entrypoint `main.go`)
- MySQL: `todo_mysql` @ `10.0.1.126:3306` (privado, TLS obrigatório)

## Por que a API é uma Function

O HeatWave só tem endpoint **privado** na VCN. O browser não alcança o banco,
então o Site fala com a Function e a Function fala com o MySQL. Auth e Storage
continuam no Appwrite; o MySQL guarda `lists`, `tasks` e `task_shares`.

```
browser ──(Appwrite SDK, sessão)──▶ Function todo-api-mysql ──(TLS + CA)──▶ MySQL 10.0.1.126
```

## Layout

| Caminho | O que é |
|---|---|
| `main.go` | entrypoint da Function (`package handler` + `func Main`, contrato Open Runtimes) |
| `internal/fn` | adapta a invocação para o `http.Handler` (uma única implementação para dois entrypoints) |
| `internal/api` | rotas e regras de permissão (owner x colaborador) |
| `internal/store` | MySQL: `tasks`, `lists`, `task_shares` |
| `internal/share` | share-by-email (os sete casos do requisito) |
| `internal/schema/sql` | migrations embutidas no binário |
| `cmd/api` | servidor local para desenvolvimento (`make run`) |
| `web/` | harness estático (não é o shell UI) |
| `.github/workflows` | test → release → **deploy** |

## Regras de dados

- `sharedWith` / `sharedNames` do requisito viram linhas em `task_shares` e são
  rematerializados na resposta — o contrato da UI não muda.
- Colaborador pode **ler** e **mover `status`**; qualquer outro campo é 403. Isso
  fecha a lacuna documentada na implementação Appwrite (PATCH cru alterava
  colunas).
- `$sequence` = `tasks.seq` (o `#N` da UI).

## Deploy: só por GitHub Actions

Nada é copiado para a VPS à mão. O único caminho é:

`.github/workflows/test-build-release.yml` → `deploy.yml` (workflow_run em `main`)

1. `scripts/deploy-function.sh` publica o código na Function (REST, `activate=true`)
2. `scripts/execute-function.sh` chama `POST /admin/migrate` com `MIGRATE_TOKEN`
   (o SQL é embutido; não existe `mysql` no host nem SSH no pipeline)
3. smoke: `/health` responde e `/tasks` anônimo devolve 401
4. opcional: `scripts/deploy-site.sh` publica `web/` como Site, se
   `APPWRITE_SITE_ID` estiver definido nas variables do repo

Secrets do repo: `APPWRITE_ENDPOINT`, `APPWRITE_PROJECT_ID`, `APPWRITE_API_KEY`,
`APPWRITE_FUNCTION_ID`, `MIGRATE_TOKEN`. Variable: `APPWRITE_SITE_ID` (opcional).

## Local

```bash
go test ./...
MYSQL_PASSWORD=... MYSQL_CA_PATH=/etc/mysql-heatwave/ca.pem \
  ALLOW_DEV_USER=true MIGRATE_TOKEN=dev make run
MIGRATE_TOKEN=dev make migrate     # aplica o schema via POST /admin/migrate
BASE=http://127.0.0.1:3000 scripts/smoke.sh
```

## Specs

`specs/product/SCOPE_LATEST.yaml`, `specs/release-plan.yaml`,
`specs/epics/e01-todo-api-mysql`, `specs/epics/e02-site-and-share`.
