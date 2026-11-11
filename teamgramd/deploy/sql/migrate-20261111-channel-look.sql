-- How a channel or supergroup looks (task 078): the accent color of its name
-- and of its profile, each with a pattern of custom emoji
-- (channels.updateColor; channel.color and channel.profile_color), and its
-- emoji status (channels.updateEmojiStatus; channel.emoji_status).
-- A color of -1 is none: the clients then take the color from the id.
--
-- One ALTER adds every column, so a database has all of them or none; the
-- check on the first makes the file safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'color') = 0,
  CONCAT('ALTER TABLE `channels`',
    ' ADD COLUMN `color` int(11) NOT NULL DEFAULT ''-1'',',
    ' ADD COLUMN `color_background_emoji_id` bigint(20) NOT NULL DEFAULT ''0'',',
    ' ADD COLUMN `profile_color` int(11) NOT NULL DEFAULT ''-1'',',
    ' ADD COLUMN `profile_color_background_emoji_id` bigint(20) NOT NULL DEFAULT ''0'',',
    ' ADD COLUMN `emoji_status_document_id` bigint(20) NOT NULL DEFAULT ''0'',',
    ' ADD COLUMN `emoji_status_until` int(11) NOT NULL DEFAULT ''0'',',
    ' ALGORITHM = INSTANT'),
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
