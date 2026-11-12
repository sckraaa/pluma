-- Stories (task 084, ADR 0054).
--
-- stories: one row per story of a user (peer_type 2) or of a channel
-- (peer_type 4). story_id is counted per peer by story_counters and is never
-- used again. A story is in the feeds while expire_date is ahead; after it
-- the row stays as the archive of its owner, and as a story of the profile
-- when pinned. active is cleared by the expiry worker once it has told the
-- owner's services that the story is over. privacy, entities, media,
-- media_areas, fwd_from and music are JSON of the schema's objects. A repeated
-- stories.sendStory finds its row by (peer, sender, random_id).
-- mentions are the users a story mentions (JSON of their ids), and
-- mentions_pending is set while they have not been sent its message yet.
--
-- story_counters: the last id of a peer; dirty is set, in the transaction of
-- the change, when the stories of the peer in the feeds changed, until the
-- services that show the peer have been told (the stories service clears
-- it, and its worker repeats what a failed request left).
--
-- story_views: who opened a story, and the reaction of that viewer if any
-- (reaction is the key of pkg/reactions). story_reads: up to which story of
-- a peer a user has read. story_hidden_peers: the channels whose stories a
-- user moved to the archive row (those of users are a flag of the contact).
-- story_stealth: the stealth mode of a user.
--
-- story_blocked: the users a user hides its stories from (contacts.block
-- with my_stories_from), apart from its main blocklist. story_shares: the
-- messages a story was shared in (inputMediaStory), one row per sender and
-- random_id, so that a repeated request counts the share once.
--
-- New tables only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `stories` (
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `story_id` int(11) NOT NULL,
  `random_id` bigint(20) NOT NULL,
  `sender_user_id` bigint(20) NOT NULL,
  `date` int(11) NOT NULL,
  `expire_date` int(11) NOT NULL,
  `active` tinyint(1) NOT NULL DEFAULT '1',
  `pinned` tinyint(1) NOT NULL DEFAULT '0',
  `pinned_to_top` int(11) NOT NULL DEFAULT '0',
  `noforwards` tinyint(1) NOT NULL DEFAULT '0',
  `edited` tinyint(1) NOT NULL DEFAULT '0',
  `privacy` text NOT NULL,
  `caption` text NOT NULL,
  `entities` mediumtext NOT NULL,
  `media` mediumtext NOT NULL,
  `media_areas` mediumtext NOT NULL,
  `fwd_from` text NOT NULL,
  `music` mediumtext NOT NULL,
  `fwd_peer_type` int(11) NOT NULL DEFAULT '0',
  `fwd_peer_id` bigint(20) NOT NULL DEFAULT '0',
  `fwd_story_id` int(11) NOT NULL DEFAULT '0',
  `forwards_count` int(11) NOT NULL DEFAULT '0',
  `mentions` text NOT NULL,
  `mentions_pending` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`peer_type`, `peer_id`, `story_id`),
  UNIQUE KEY `random` (`peer_type`, `peer_id`, `sender_user_id`, `random_id`),
  KEY `feed` (`peer_type`, `peer_id`, `expire_date`),
  KEY `expiry` (`active`, `expire_date`),
  KEY `mentions` (`mentions_pending`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `story_counters` (
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `last_id` int(11) NOT NULL,
  `dirty` tinyint(1) NOT NULL DEFAULT '0',
  `dirty_at` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`peer_type`, `peer_id`),
  KEY `dirty` (`dirty`, `dirty_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `story_views` (
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `story_id` int(11) NOT NULL,
  `viewer_id` bigint(20) NOT NULL,
  `date` int(11) NOT NULL,
  `reaction` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `reaction_date` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`peer_type`, `peer_id`, `story_id`, `viewer_id`),
  KEY `viewer` (`viewer_id`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `story_reads` (
  `user_id` bigint(20) NOT NULL,
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `max_read_id` int(11) NOT NULL,
  PRIMARY KEY (`user_id`, `peer_type`, `peer_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `story_hidden_peers` (
  `user_id` bigint(20) NOT NULL,
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `peer_type`, `peer_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `story_stealth` (
  `user_id` bigint(20) NOT NULL,
  `active_until` int(11) NOT NULL DEFAULT '0',
  `cooldown_until` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `story_blocked` (
  `user_id` bigint(20) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `date` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`user_id`, `peer_id`),
  KEY `list` (`user_id`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `story_shares` (
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `story_id` int(11) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `random_id` bigint(20) NOT NULL,
  `date` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`peer_type`, `peer_id`, `story_id`, `user_id`, `random_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
