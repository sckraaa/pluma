-- A supergroup turned into a broadcast group (task 080,
-- channels.convertToGigagroup, channel.gigagroup): only its admins write.
-- The conversion is one way, so the flag is never cleared.
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'gigagroup') = 0,
  'ALTER TABLE `channels` ADD COLUMN `gigagroup` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
