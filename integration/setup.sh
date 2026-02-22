#!/bin/bash
set -euo pipefail

DB_PATH="/var/lib/netbird/store.db"

echo "Waiting for database schema..."
for i in $(seq 1 60); do
    if [ -f "$DB_PATH" ] && sqlite3 "$DB_PATH" "SELECT name FROM sqlite_master WHERE type='table' AND name='accounts';" 2>/dev/null | grep -q accounts; then
        echo "Database schema detected"
        break
    fi
    echo "Waiting for database... ($i/60)"
    sleep 1
done

if [ ! -f "$DB_PATH" ]; then
    echo "ERROR: Database not found after 60 seconds"
    exit 1
fi

apt-get update -qq && apt-get install -y -qq sqlite3 > /dev/null 2>&1

echo "Seeding database..."
sqlite3 "$DB_PATH" <<'SQL'
-- Account
INSERT OR IGNORE INTO accounts (id, created_by, created_at, domain, domain_category, is_domain_primary_account, network_identifier, network_serial, settings_peer_login_expiration_enabled, settings_peer_login_expiration, settings_regular_users_view_blocked, settings_groups_propagation_enabled, settings_jwt_groups_enabled, settings_extra_peer_approval_enabled, network_net)
VALUES ('account1', 'user1', '2024-04-17 09:35:50+00:00', 'netbird.selfhosted', 'private', 1, 'network1', 1, 1, 86400000000000, 0, 1, 0, 0, '{"IP":"100.64.0.0","Mask":"//8AAA=="}');

-- User (owner)
INSERT OR IGNORE INTO users (id, account_id, role, is_service_user, non_deletable, blocked, created_at, issued)
VALUES ('user1', 'account1', 'owner', 0, 0, 0, '2024-08-12 00:00:00', 'api');

-- "All" group
INSERT OR IGNORE INTO groups (id, account_id, name, issued, integration_ref_id, integration_ref_integration_type)
VALUES ('group-all', 'account1', 'All', 'api', 0, NULL);

-- PAT: plain token is nbp_GRADBFAcX04gihh9ZwL2gGeJ55kbK63iBeI0
INSERT OR IGNORE INTO personal_access_tokens (id, user_id, name, hashed_token, expiration_date, created_by, created_at, last_used)
VALUES ('1', 'user1', 'Test API Key', '2dJnUsNFEF0BWoxKme6g7meeXtt8ipOnj18Jpt1cOtc=', '2124-08-12 00:00:00', 'user1', '2024-08-12 00:00:00', NULL);
SQL

# Verify
ACCOUNT_COUNT=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM accounts;")
PAT_COUNT=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM personal_access_tokens;")
echo "Accounts: $ACCOUNT_COUNT, PATs: $PAT_COUNT"

if [ "$PAT_COUNT" -lt 1 ]; then
    echo "ERROR: Seed verification failed"
    exit 1
fi

echo "Database seeded successfully"
