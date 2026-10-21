-- PFS (task 001, WP3): temporary auth keys expire.
-- expires_at is a unix time; 0 means the key does not expire (permanent keys
-- only). authsession sets it from the handshake's expires_in and lowers it to
-- the expires_at the client sends in auth.bindTempAuthKey; an expired
-- temporary key is treated as unknown (transport error -404), so the client
-- creates a new one.

ALTER TABLE `auth_key_infos` ADD `expires_at` bigint(20) NOT NULL DEFAULT '0' AFTER `media_temp_auth_key_id`;

-- Temporary keys from before this migration (auth_key_type 1 temp, 2 media
-- temp) would never expire, and their bindings were written by the old
-- auth.bindTempAuthKey, which checked nothing (audit 02, #3 and #4): any of
-- them may route a stranger's key to someone else's account. They are
-- expired and unbound here, and permanent keys forget them. Clients get -404
-- for them and create and bind new temporary keys; permanent keys and logins
-- are kept. On a fresh database this changes no rows.
UPDATE `auth_key_infos` SET `perm_auth_key_id` = 0, `expires_at` = UNIX_TIMESTAMP() WHERE `auth_key_type` IN (1, 2);
UPDATE `auth_key_infos` SET `temp_auth_key_id` = 0, `media_temp_auth_key_id` = 0 WHERE `auth_key_type` = 0;
