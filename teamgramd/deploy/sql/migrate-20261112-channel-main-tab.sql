-- The tab a channel's profile opens on (task 078, channels.setMainProfileTab,
-- channelFull.main_tab): the kind of profileTab* as mtproto.ProfileTabType*
-- numbers it (users.main_tab keeps a user's the same way), 0 for none.
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'main_tab') = 0,
  'ALTER TABLE `channels` ADD COLUMN `main_tab` int(11) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
