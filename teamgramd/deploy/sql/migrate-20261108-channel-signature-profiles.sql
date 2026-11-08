-- Signatures that link to the authors' profiles (task 045).
--
-- channels.signature_profiles: the posts of a broadcast channel carry their
-- author (from_id) and link to the author's profile, as messages of a group
-- do (channel.signature_profiles, channels.toggleSignatures). It is set only
-- with signatures; turning signatures off clears it.
--
-- Safe on an empty and on a populated database, and safe to run again: the
-- column is added only when it is missing.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'signature_profiles') = 0,
  'ALTER TABLE `channels` ADD COLUMN `signature_profiles` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
