-- The accounts of the inline bots the server answers for itself (task 099,
-- ADR 0062): @gif (1000101) and @pic (1000102), the registry of
-- pkg/inlinebots. The clients find them by the usernames of help.getConfig
-- (teamgramd/bin/config.json). This file sorts after z_init.sql. Safe to run
-- again.
--
-- Each bot is a bot account (user_type 3, is_bot 1, as botstore.Create makes
-- one) with a service number outside the format of real ones, as 42001 of
-- the antispam account; no client sees it (a bot's phone is hidden). Its
-- username row, and a bots row with the placeholder of the inline mode, no
-- chats (bot_nochats) and no request for the location (bot_inline_geo).
--
-- An account is made only while its username is free or already its own: if
-- a person or a channel has taken the name, nothing of that bot is written,
-- and bff bots reports it at start (inline_bot_misconfigured) and answers
-- BOT_INLINE_DISABLED for it; docs/ops/inline-bots.md says what to do. The
-- username table compares names without case.
--
-- bots.token is unique and holds the hex SHA-256 of a token's secret
-- (64 characters, pkg/botstore): "builtin:<id>" is unique and matches no
-- token, so nobody signs in as these bots.
--
-- The placeholder is U+1F50D (LEFT-POINTING MAGNIFYING GLASS), written as its
-- UTF-8 bytes so that the character set of the client running the file does
-- not matter.

INSERT IGNORE INTO users (id, user_type, access_hash, first_name, username, phone, country_code, is_bot, account_days_ttl, created_at, updated_at)
SELECT 1000101, 3, 5690652051647993109, 'GIF', 'gif', '42101', '', 1, 548, '2026-11-15 00:00:00', '2026-11-15 00:00:00' FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM username WHERE username = 'gif' AND NOT (peer_type = 2 AND peer_id = 1000101));

INSERT IGNORE INTO users (id, user_type, access_hash, first_name, username, phone, country_code, is_bot, account_days_ttl, created_at, updated_at)
SELECT 1000102, 3, 4718041268057259926, 'Pic', 'pic', '42102', '', 1, 548, '2026-11-15 00:00:00', '2026-11-15 00:00:00' FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM username WHERE username = 'pic' AND NOT (peer_type = 2 AND peer_id = 1000102));

INSERT IGNORE INTO username (username, peer_type, peer_id)
SELECT username, 2, id FROM users WHERE id IN (1000101, 1000102) AND is_bot = 1;

INSERT IGNORE INTO bots (bot_id, creator_user_id, token, bot_nochats, bot_inline_geo, bot_inline_placeholder)
SELECT id, 0, CONCAT('builtin:', id), 1, 0, CONVERT(X'F09F948D' USING utf8mb4)
FROM users WHERE id IN (1000101, 1000102) AND is_bot = 1;
