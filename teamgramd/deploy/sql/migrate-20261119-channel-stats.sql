-- The statistics of channels, supergroups, posts, stories and polls (task
-- 103, ADR 0066): the events no other table keeps, written from now on in
-- the transaction of the change each records (pkg/channelstats), and the
-- indexes the statistics read the existing tables by, a channel and a
-- range of dates at a time.
--
-- stats_collection: since when each kind of events is recorded. Its rows
-- are written once (INSERT IGNORE): running the file again keeps the date
-- of the first run, and the graphs of the events start there.
--
-- Safe on an empty and on a populated database, and safe to run again. The
-- indexes are built online (ALGORITHM = INPLACE, LOCK = NONE): writes go on
-- while they are built.

CREATE TABLE IF NOT EXISTS `stats_collection` (
  `kind` varchar(32) NOT NULL COMMENT 'member_events, mute_events, view_sources, public_forwards, readers, admin_actions',
  `since` bigint(20) NOT NULL COMMENT 'unix seconds, the first run of this migration',
  PRIMARY KEY (`kind`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Since when each kind of statistics events is recorded';

INSERT IGNORE INTO `stats_collection` (`kind`, `since`) VALUES
  ('member_events', UNIX_TIMESTAMP()),
  ('mute_events', UNIX_TIMESTAMP()),
  ('view_sources', UNIX_TIMESTAMP()),
  ('public_forwards', UNIX_TIMESTAMP()),
  ('readers', UNIX_TIMESTAMP()),
  ('admin_actions', UNIX_TIMESTAMP());

-- One row per join or leave of a member of a channel or supergroup. A join
-- has its source (pkg/channelstats JoinSource) and who let the member in.
-- A ghost (ADR 0048) has no rows.
CREATE TABLE IF NOT EXISTS `channel_stats_member_events` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `channel_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `date` bigint(20) NOT NULL,
  `joined` tinyint(1) NOT NULL COMMENT '1 joined, 0 left or removed',
  `source` tinyint(4) NOT NULL DEFAULT '0' COMMENT 'of a join: 1 link, 2 folder, 3 request, 4 public, 5 added, 0 other',
  `inviter_id` bigint(20) NOT NULL DEFAULT '0' COMMENT 'the link admin, the approving admin or the member who added',
  PRIMARY KEY (`id`),
  KEY `channel_date` (`channel_id`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Joins and leaves of channel members';

-- One row per change of the muting in effect of a member of a channel.
CREATE TABLE IF NOT EXISTS `channel_stats_mute_events` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `channel_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `date` bigint(20) NOT NULL,
  `mute_until` int(11) NOT NULL COMMENT '0 notifications on; later than date muted until; 2147483647 for good',
  PRIMARY KEY (`id`),
  KEY `channel_date` (`channel_id`, `date`),
  KEY `channel_user` (`channel_id`, `user_id`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Changes of the muting of channels by their members';

-- Where a post was viewed from, set by its first view (pkg/channelstats
-- ViewSource): 1 subscriber, 2 channel, 3 group, 4 private chat, 0 other.
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_message_views' AND COLUMN_NAME = 'source') = 0,
  'ALTER TABLE `channel_message_views` ADD COLUMN `source` tinyint(4) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_message_views' AND INDEX_NAME = 'channel_date') = 0,
  'ALTER TABLE `channel_message_views` ADD KEY `channel_date` (`channel_id`, `date`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

-- The forwards of posts (src_kind 1) and stories (src_kind 2) into channels
-- and supergroups, one row per copy. Whether the copy's channel is public is
-- read when they are listed.
CREATE TABLE IF NOT EXISTS `channel_stats_public_forwards` (
  `src_kind` tinyint(4) NOT NULL COMMENT '1 post of a channel, 2 story',
  `src_peer_type` int(11) NOT NULL,
  `src_peer_id` bigint(20) NOT NULL,
  `src_id` int(11) NOT NULL COMMENT 'the id of the post or the story',
  `dst_channel_id` bigint(20) NOT NULL,
  `dst_message_id` int(11) NOT NULL,
  `date` bigint(20) NOT NULL,
  PRIMARY KEY (`src_kind`, `src_peer_type`, `src_peer_id`, `src_id`, `dst_channel_id`, `dst_message_id`),
  KEY `src_date` (`src_kind`, `src_peer_type`, `src_peer_id`, `src_id`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Forwards of channel posts and stories into channels';

-- Who read or wrote to a supergroup on which UTC day (floor(unix / 86400)).
CREATE TABLE IF NOT EXISTS `channel_stats_readers` (
  `channel_id` bigint(20) NOT NULL,
  `day` int(11) NOT NULL COMMENT 'floor(unix / 86400), UTC',
  `user_id` bigint(20) NOT NULL,
  PRIMARY KEY (`channel_id`, `day`, `user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Readers of supergroups per day';

-- What each admin of a channel did on a UTC day.
CREATE TABLE IF NOT EXISTS `channel_stats_admin_actions` (
  `channel_id` bigint(20) NOT NULL,
  `day` int(11) NOT NULL COMMENT 'floor(unix / 86400), UTC',
  `admin_id` bigint(20) NOT NULL,
  `deleted` int(11) NOT NULL DEFAULT '0' COMMENT 'messages of others deleted',
  `kicked` int(11) NOT NULL DEFAULT '0' COMMENT 'users removed',
  `banned` int(11) NOT NULL DEFAULT '0' COMMENT 'members restricted',
  PRIMARY KEY (`channel_id`, `day`, `admin_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Actions of channel admins per day';

-- The existing tables, read by a channel and a range of dates.
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_messages' AND INDEX_NAME = 'channel_date') = 0,
  'ALTER TABLE `channel_messages` ADD KEY `channel_date` (`channel_id`, `date2`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_message_reactions' AND INDEX_NAME = 'channel_date') = 0,
  'ALTER TABLE `channel_message_reactions` ADD KEY `channel_date` (`channel_id`, `date`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_message_forwards' AND INDEX_NAME = 'channel_date') = 0,
  'ALTER TABLE `channel_message_forwards` ADD KEY `channel_date` (`channel_id`, `date`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'story_views' AND INDEX_NAME = 'peer_date') = 0,
  'ALTER TABLE `story_views` ADD KEY `peer_date` (`peer_type`, `peer_id`, `date`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'stories' AND INDEX_NAME = 'fwd_source') = 0,
  'ALTER TABLE `stories` ADD KEY `fwd_source` (`fwd_peer_type`, `fwd_peer_id`, `fwd_story_id`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

-- The forwards of posts made before this migration, from the copies kept
-- in channel_messages (message_data is the message as msg stores it, jsonx
-- of mtproto.Message): fwd_from.from_id a channel (peer_type 4) with
-- fwd_from.channel_post. The automatic copy of a post in its discussion
-- group is no forward: it alone has fwd_from.saved_from_peer in a channel
-- (msg channel_send.go postCopy; biz channel message_view.go tells the two
-- apart the same way). Stories forwarded before are in stories.fwd_*.
-- INSERT IGNORE: a copy recorded already stays as it is.
INSERT IGNORE INTO `channel_stats_public_forwards` (`src_kind`, `src_peer_type`, `src_peer_id`, `src_id`, `dst_channel_id`, `dst_message_id`, `date`)
SELECT 1, 4,
       CAST(JSON_UNQUOTE(JSON_EXTRACT(m.message_data, '$.fwd_from.from_id.channel_id')) AS SIGNED),
       CAST(JSON_UNQUOTE(JSON_EXTRACT(m.message_data, '$.fwd_from.channel_post.value')) AS SIGNED),
       m.channel_id, m.id, m.date2
  FROM `channel_messages` m
 WHERE m.deleted = 0
   AND JSON_EXTRACT(m.message_data, '$.fwd_from.channel_post.value') IS NOT NULL
   AND JSON_EXTRACT(m.message_data, '$.fwd_from.from_id.channel_id') IS NOT NULL
   AND JSON_EXTRACT(m.message_data, '$.fwd_from.saved_from_peer') IS NULL;
