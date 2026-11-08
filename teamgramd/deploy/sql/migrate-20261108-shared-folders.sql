-- Shared folders (task 064, chatlists.*).
--
-- chatlist_invites: a link of a folder, by its slug (https://pluma.chat/addlist/<slug>).
-- chatlist_invite_peers: the channels a link shares; link is the channel
-- invite link the shared link made for it, empty for a public channel that
-- takes anyone.
-- chatlist_seen_peers: the channels of an imported folder's link already
-- shown to its user, joined or dismissed: chatlists.getChatlistUpdates
-- returns only the channels added after them.
--
-- New tables only: safe on an empty and on a populated database, and safe to
-- run again.

CREATE TABLE IF NOT EXISTS `chatlist_invites` (
  `slug` varchar(64) COLLATE utf8mb4_bin NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `dialog_filter_id` int(11) NOT NULL,
  `title` varchar(64) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `date2` bigint(20) NOT NULL,
  PRIMARY KEY (`slug`),
  KEY `user_filter` (`user_id`, `dialog_filter_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Links of shared folders';

CREATE TABLE IF NOT EXISTS `chatlist_invite_peers` (
  `slug` varchar(64) COLLATE utf8mb4_bin NOT NULL,
  `channel_id` bigint(20) NOT NULL,
  `link` varchar(64) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `position` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`slug`, `channel_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Channels of links of shared folders';

CREATE TABLE IF NOT EXISTS `chatlist_seen_peers` (
  `user_id` bigint(20) NOT NULL,
  `dialog_filter_id` int(11) NOT NULL,
  `channel_id` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `dialog_filter_id`, `channel_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Channels of a link already shown to the importer';
