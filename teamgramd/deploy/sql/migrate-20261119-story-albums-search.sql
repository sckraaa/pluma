-- Albums of stories and the search of stories (task 104, ADR 0069).
--
-- story_albums: the albums of a user (peer_type 2) or of a channel
-- (peer_type 4). album_id is counted per peer by story_counters.last_album_id
-- and is never used again, so a link to a deleted album does not open a new
-- one. position orders the albums of the profile, from 1.
--
-- story_album_items: the stories of an album, ordered by position. A story
-- may be in several albums, pinned to the profile or not. The rows of a story
-- go in the transaction that deletes it, those of an album with the album.
--
-- story_tags: the hashtags (kind 1) and cashtags (kind 2) of the caption of a
-- story, lower case and without the sign. story_places: its venues (kind 1,
-- venue_key is provider ':' venue_id) and its geo points with an address
-- (kind 2, the address lower case); seq is the index of the area in
-- stories.media_areas. In both, everyone is 1 for a user's story for
-- everyone (exceptions allowed) and always for a channel's; date is the date
-- of the story.
--
-- story_counters.last_album_id: the last album id of a peer.
-- stories.search_indexed: 1 once the tags and places of the story are in the
-- index; the stories of before this migration are indexed by the stories
-- worker.
--
-- All changes of these rows run under the lock of the peer's story_counters
-- row, like every change of its stories.
--
-- New tables and guarded columns: safe on an empty and on a populated
-- database, and safe to run again.

CREATE TABLE IF NOT EXISTS `story_albums` (
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `album_id` int(11) NOT NULL,
  `title` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `position` bigint(20) NOT NULL,
  `created_at` int(11) NOT NULL,
  PRIMARY KEY (`peer_type`, `peer_id`, `album_id`),
  KEY `ordered` (`peer_type`, `peer_id`, `position`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `story_album_items` (
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `album_id` int(11) NOT NULL,
  `story_id` int(11) NOT NULL,
  `position` bigint(20) NOT NULL,
  PRIMARY KEY (`peer_type`, `peer_id`, `album_id`, `story_id`),
  KEY `ordered` (`peer_type`, `peer_id`, `album_id`, `position`),
  KEY `story` (`peer_type`, `peer_id`, `story_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `story_tags` (
  `kind` tinyint(4) NOT NULL,
  `tag` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `story_id` int(11) NOT NULL,
  `everyone` tinyint(1) NOT NULL,
  `date` int(11) NOT NULL,
  PRIMARY KEY (`kind`, `tag`, `peer_type`, `peer_id`, `story_id`),
  KEY `found` (`kind`, `tag`, `everyone`, `date`),
  KEY `story` (`peer_type`, `peer_id`, `story_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `story_places` (
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `story_id` int(11) NOT NULL,
  `seq` int(11) NOT NULL,
  `kind` tinyint(4) NOT NULL,
  `venue_key` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `country` varchar(8) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `state` varchar(128) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `city` varchar(128) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `street` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `everyone` tinyint(1) NOT NULL,
  `date` int(11) NOT NULL,
  PRIMARY KEY (`peer_type`, `peer_id`, `story_id`, `seq`),
  KEY `venue` (`kind`, `venue_key`, `date`),
  KEY `geo` (`kind`, `country`, `city`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'story_counters' AND COLUMN_NAME = 'last_album_id') = 0,
  'ALTER TABLE `story_counters` ADD COLUMN `last_album_id` int(11) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'stories' AND COLUMN_NAME = 'search_indexed') = 0,
  'ALTER TABLE `stories` ADD COLUMN `search_indexed` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'stories' AND INDEX_NAME = 'unindexed') = 0,
  'ALTER TABLE `stories` ADD KEY `unindexed` (`search_indexed`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
