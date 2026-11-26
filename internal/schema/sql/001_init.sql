-- 001_init.sql — schema for the MySQL-backed Todo canary.
-- Mirrors the Appwrite TablesDB model in specs/product/TODO_APP_REQUIREMENTS.md
-- (tables `lists` and `tasks`) with the document-array fields normalised:
-- `sharedWith` / `sharedNames` become rows in `task_shares`.
--
-- Apply with: make migrate   (scripts/migrate.sh)

CREATE TABLE IF NOT EXISTS lists (
    id         CHAR(36)     NOT NULL PRIMARY KEY,
    seq        BIGINT UNSIGNED NOT NULL AUTO_INCREMENT UNIQUE,
    name       VARCHAR(120) NOT NULL,
    color      VARCHAR(32)  NOT NULL DEFAULT 'violet',
    user_id    VARCHAR(64)  NOT NULL,
    created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    KEY by_user (user_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS tasks (
    id            CHAR(36)     NOT NULL PRIMARY KEY,
    seq           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT UNIQUE,   -- shown in the UI as #N
    title         VARCHAR(255) NOT NULL,
    notes         TEXT         NULL,
    status        ENUM('open','doing','review','done') NOT NULL DEFAULT 'open',
    priority      ENUM('high','mid','low')             NULL,
    due_date      DATETIME(3)  NULL,
    list_id       CHAR(36)     NULL,
    user_id       VARCHAR(64)  NOT NULL,                            -- owner
    owner_name    VARCHAR(120) NULL,
    attachment_id VARCHAR(64)  NULL,
    created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    KEY by_user_status (user_id, status),
    KEY by_list (list_id),
    FULLTEXT KEY title_search (title, notes)
) ENGINE=InnoDB;

-- Collaborators. The requirement keeps `sharedWith`/`sharedNames` as arrays on the
-- row; here they are the authoritative storage and the API re-materialises both
-- arrays on every read so the UI contract does not change.
CREATE TABLE IF NOT EXISTS task_shares (
    task_id    CHAR(36)     NOT NULL,
    user_id    VARCHAR(64)  NOT NULL,
    user_name  VARCHAR(120) NULL,
    created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (task_id, user_id),
    KEY by_user (user_id),
    CONSTRAINT fk_share_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE
) ENGINE=InnoDB;
