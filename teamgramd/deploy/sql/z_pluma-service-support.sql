-- The service account (777000) is a support account, as Telegram's is (task
-- 017): iOS shows "service notifications" under its name by that flag and a
-- last seen time without it; the other clients know the account by its id.
-- This file sorts after z_pluma-service-account.sql. Safe to run again.
--
-- The cached user stays in Redis until it expires or is deleted:
--   redis-cli -a "$REDIS_PASSWORD" DEL 'user_data.3#777000'
UPDATE users SET support = 1 WHERE id = 777000;
