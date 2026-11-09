-- channels.forum_sidebar: the topics of a forum shown in a sidebar (layer 233,
-- channel flags2.23, set by channels.toggleForum). Safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'forum_sidebar') = 0,
  'ALTER TABLE `channels` ADD COLUMN `forum_sidebar` tinyint(1) NOT NULL DEFAULT ''0'' AFTER `forum_tabs`, ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
