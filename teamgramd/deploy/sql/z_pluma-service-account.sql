-- The service account (777000) of z_init.sql is Pluma, not Teamgram (task 017):
-- its name and its username. This file sorts after z_init.sql, so a new
-- database gets the account from z_init.sql first and this renames it, as on
-- an existing one. Safe to run again.
--
-- The cached user stays in Redis until it expires or is deleted:
--   redis-cli -a "$REDIS_PASSWORD" DEL 'user_data.3#777000'
UPDATE users SET first_name = 'Pluma', username = 'pluma'
WHERE id = 777000 AND first_name = 'Teamgram' AND username = 'teamgram';
