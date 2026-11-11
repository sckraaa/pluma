-- The support ghost (task 080, ADR 0048): an account that takes part in
-- chats without being seen among their members.
--   users.ghost                 the account is a ghost (pluma-admin ghost);
--   channel_participants.hidden the row is a ghost's: a member for delivery,
--   chat_participants.hidden    history and rights, left out of what the
--                               other members are shown (lists, counters,
--                               service messages, recent actions, reads).
-- A live row of an account has hidden = users.ghost.
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'ghost') = 0,
  'ALTER TABLE `users` ADD COLUMN `ghost` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_participants' AND COLUMN_NAME = 'hidden') = 0,
  'ALTER TABLE `channel_participants` ADD COLUMN `hidden` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'chat_participants' AND COLUMN_NAME = 'hidden') = 0,
  'ALTER TABLE `chat_participants` ADD COLUMN `hidden` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
