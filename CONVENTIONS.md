# Convenções

- Conventional Commits; sem `Co-authored-by`.
- Deploy **só** por GitHub Actions (`.github/workflows/deploy.yml`). Não existe
  passo manual de build, cópia para a VPS ou `docker` no host.
- Schema: só por `internal/schema/sql/*.sql` + `POST /admin/migrate` (token).
- Segredos: variáveis da Function (Console) e secrets do GitHub. Nada em repo.
  A CA do endpoint entra em `MYSQL_CA_PEM`.
- Regra de permissão nova → teste em `internal/api/api_test.go`.
