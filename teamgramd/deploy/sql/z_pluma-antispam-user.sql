-- The account of the antispam of supergroups (task 082, ADR 0050): the
-- recent actions name it as the one who deleted a spam message, and the
-- clients know such a deletion by its id, telegram_antispam_user_id of the
-- app config (teamgramd/bin/app_config.json). A service account
-- (user_type 4) that nobody signs in to: no username, a service number
-- outside the format of real ones, as 42777 of 777000; it is a member of no
-- chat. This file sorts after z_init.sql. Safe to run again.
INSERT IGNORE INTO users (id, user_type, access_hash, secret_key_id, first_name, last_name, username, phone, country_code, verified, support, scam, fake, premium, about, state, is_bot, account_days_ttl, photo_id, restricted, restriction_reason, archive_and_mute_new_noncontact_peers, emoji_status_document_id, emoji_status_until, deleted, delete_reason, created_at, updated_at)
VALUES (1000001, 4, 7362104589126633471, 0, 'Pluma Anti-Spam', '', '', '42001', '', 0, 0, 0, 0, 0, '', 0, 0, 180, 0, 0, '', 0, 0, 0, 0, '', '2026-11-11 00:00:00', '2026-11-11 00:00:00');
