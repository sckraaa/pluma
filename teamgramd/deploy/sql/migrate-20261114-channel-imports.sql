-- The import of a group from a Telegram Desktop export (task 083, ADR 0051).
--
-- channel_imports: one row per import: the chat of the export, the channel
-- it is written into, its creator and how far it is. state: 0 running,
-- 1 finished, 2 aborted. One chat has at most one import that is not
-- aborted (active_source_chat_id is NULL for an aborted one, and NULL never
-- conflicts in a unique key). mapping is the JSON of the authors already
-- decided, export key to Pluma user id (0: skipped): an import that is
-- resumed may add authors and may not change one.
--
-- channel_import_messages: the journal of an import, written in the
-- transaction that writes the messages. seq is the record's place in the
-- export, from 1 (0: the creation message the import adds when the export
-- has none), source_id its id in Telegram, message_id its id in the channel
-- (0: not imported, skip_reason says why). The next batch must start at
-- MAX(seq) + 1, so a batch is never written twice.
--
-- channel_import_media: the files already uploaded for a chat of an export,
-- by the hash of their path and size, with the media made of them: a
-- resumed import does not upload a file again.
--
-- channels.imported_max_id: the last message of an imported history. The
-- voice and round video messages up to it are played for everyone
-- (pkg/channelcontents): the export does not tell who played what.
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `channel_imports` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `source_type` varchar(32) COLLATE utf8mb4_bin NOT NULL,
  `source_chat_id` bigint(20) NOT NULL,
  `active_source_chat_id` bigint(20) GENERATED ALWAYS AS (IF(`state` <> 2, `source_chat_id`, NULL)) VIRTUAL,
  `channel_id` bigint(20) NOT NULL,
  `creator_user_id` bigint(20) NOT NULL,
  `state` tinyint(4) NOT NULL DEFAULT '0',
  `messages` bigint(20) NOT NULL DEFAULT '0',
  `skipped` bigint(20) NOT NULL DEFAULT '0',
  `mapping` mediumtext COLLATE utf8mb4_bin NOT NULL,
  `date2` bigint(20) NOT NULL DEFAULT '0',
  `finished_at` bigint(20) NOT NULL DEFAULT '0',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `active_source` (`source_type`, `active_source_chat_id`),
  KEY `channel` (`channel_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Imports of groups from exports';

CREATE TABLE IF NOT EXISTS `channel_import_messages` (
  `import_id` bigint(20) NOT NULL,
  `seq` bigint(20) NOT NULL,
  `source_id` bigint(20) NOT NULL,
  `message_id` int(11) NOT NULL DEFAULT '0',
  `skip_reason` varchar(64) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  PRIMARY KEY (`import_id`, `seq`),
  UNIQUE KEY `source` (`import_id`, `source_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Journal of an import: export record to channel message';

CREATE TABLE IF NOT EXISTS `channel_import_media` (
  `source_type` varchar(32) COLLATE utf8mb4_bin NOT NULL,
  `source_chat_id` bigint(20) NOT NULL,
  `path_hash` binary(32) NOT NULL,
  `size` bigint(20) NOT NULL,
  `media_data` mediumtext COLLATE utf8mb4_bin NOT NULL,
  `date2` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`source_type`, `source_chat_id`, `path_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Files of an export already uploaded';

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'imported_max_id') = 0,
  'ALTER TABLE `channels` ADD COLUMN `imported_max_id` int(11) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
