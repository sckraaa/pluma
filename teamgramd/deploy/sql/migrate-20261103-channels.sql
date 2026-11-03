-- Channels and supergroups (ADR 0034, task 036): one copy of each message
-- per channel, the channel's own pts and journal, its participants.
--
-- channels is the channel's row and its counters, as user_state is a
-- user's: pts (base 1: the first update gets 2), last_message_id (ids go
-- one by one inside the channel, the first message is 1) and date2 change
-- only in pkg/channelbox, under the row's lock, with the journal rows of
-- channel_pts_updates and the data they describe.
--
-- New tables only: safe on an empty and on a populated database, and safe to
-- run again.

CREATE TABLE IF NOT EXISTS `channels` (
  `id` bigint(20) NOT NULL,
  `access_hash` bigint(20) NOT NULL,
  `creator_user_id` bigint(20) NOT NULL,
  `title` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `about` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `photo_id` bigint(20) NOT NULL DEFAULT '0',
  `broadcast` tinyint(1) NOT NULL DEFAULT '0',
  `megagroup` tinyint(1) NOT NULL DEFAULT '0',
  `forum` tinyint(1) NOT NULL DEFAULT '0',
  `signatures` tinyint(1) NOT NULL DEFAULT '0',
  `pts` int(11) NOT NULL DEFAULT '1',
  `last_message_id` int(11) NOT NULL DEFAULT '0',
  `top_message` int(11) NOT NULL DEFAULT '0',
  `date2` bigint(20) NOT NULL DEFAULT '0',
  `participants_count` int(11) NOT NULL DEFAULT '0',
  `admins_count` int(11) NOT NULL DEFAULT '0',
  `kicked_count` int(11) NOT NULL DEFAULT '0',
  `banned_count` int(11) NOT NULL DEFAULT '0',
  `default_banned_rights` int(11) NOT NULL DEFAULT '0',
  `slowmode_seconds` int(11) NOT NULL DEFAULT '0',
  `hidden_prehistory` tinyint(1) NOT NULL DEFAULT '0',
  `linked_chat_id` bigint(20) NOT NULL DEFAULT '0',
  `migrated_from_chat_id` bigint(20) NOT NULL DEFAULT '0',
  `migrated_from_max_id` int(11) NOT NULL DEFAULT '0',
  `noforwards` tinyint(1) NOT NULL DEFAULT '0',
  `join_to_send` tinyint(1) NOT NULL DEFAULT '0',
  `join_request` tinyint(1) NOT NULL DEFAULT '0',
  `participants_hidden` tinyint(1) NOT NULL DEFAULT '0',
  `available_reactions_type` int(11) NOT NULL DEFAULT '0',
  `available_reactions` json DEFAULT NULL,
  `deleted` tinyint(1) NOT NULL DEFAULT '0',
  `version` int(11) NOT NULL DEFAULT '1',
  `date` bigint(20) NOT NULL DEFAULT '0',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `migrated_from` (`migrated_from_chat_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Channels and supergroups: metadata and the committed counters of the channel box';

-- One copy of each message of a channel. pts is that of the journal row
-- that added it. out_random_id keeps a client's random_id of a message
-- for good (a resent request finds the message instead of a second copy);
-- service messages have random_id 0, and NULL never conflicts in a unique
-- key. The view of a message (out, mentioned, media_unread) is the viewer's
-- and is not stored here.
CREATE TABLE IF NOT EXISTS `channel_messages` (
  `channel_id` bigint(20) NOT NULL,
  `id` int(11) NOT NULL,
  `sender_user_id` bigint(20) NOT NULL,
  `random_id` bigint(20) NOT NULL DEFAULT '0',
  `out_random_id` bigint(20) GENERATED ALWAYS AS (IF(`random_id` <> 0, `random_id`, NULL)) VIRTUAL,
  `pts` int(11) NOT NULL,
  `message_filter_type` int(11) NOT NULL DEFAULT '0',
  `message_data` json DEFAULT NULL,
  `message` varchar(6000) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `pinned` tinyint(1) NOT NULL DEFAULT '0',
  `date2` bigint(20) NOT NULL DEFAULT '0',
  `deleted` tinyint(1) NOT NULL DEFAULT '0',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`channel_id`, `id`),
  UNIQUE KEY `sender_out_random` (`channel_id`, `sender_user_id`, `out_random_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Messages of channels, one copy per channel';

-- The journal of a channel box: each row covers the pts (pts - pts_count,
-- pts] and holds the whole update (an edit: the whole message after it).
-- Only the five updates with the channel's pts are journaled
-- (updateNewChannelMessage, updateEditChannelMessage,
-- updateDeleteChannelMessages, updateChannelWebPage,
-- updatePinnedChannelMessages).
CREATE TABLE IF NOT EXISTS `channel_pts_updates` (
  `channel_id` bigint(20) NOT NULL,
  `pts` int(11) NOT NULL,
  `pts_count` int(11) NOT NULL,
  `update_type` int(11) NOT NULL,
  `update_data` json NOT NULL,
  `date2` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`channel_id`, `pts`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Journal of each channel box';

-- A user in a channel, now or before. role: 0 member, 1 admin, 2 creator.
-- state: 0 in the channel, 1 left, 2 banned (banned_rights with
-- view_messages: kicked; without it: restricted and still in). admin_rights
-- and banned_rights are the flags of chatAdminRights and chatBannedRights.
-- available_min_id hides the messages before it from this user (hidden
-- prehistory for those who join, the history deleted for oneself).
-- last_send_date is for the slow mode.
CREATE TABLE IF NOT EXISTS `channel_participants` (
  `channel_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `role` tinyint(4) NOT NULL DEFAULT '0',
  `state` tinyint(4) NOT NULL DEFAULT '0',
  `admin_rights` int(11) NOT NULL DEFAULT '0',
  `rank` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `banned_rights` int(11) NOT NULL DEFAULT '0',
  `until_date` int(11) NOT NULL DEFAULT '0',
  `inviter_user_id` bigint(20) NOT NULL DEFAULT '0',
  `promoted_by` bigint(20) NOT NULL DEFAULT '0',
  `kicked_by` bigint(20) NOT NULL DEFAULT '0',
  `available_min_id` int(11) NOT NULL DEFAULT '0',
  `last_send_date` bigint(20) NOT NULL DEFAULT '0',
  `date` bigint(20) NOT NULL DEFAULT '0',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`channel_id`, `user_id`),
  KEY `user_channels` (`user_id`, `state`),
  KEY `channel_members` (`channel_id`, `state`, `role`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Participants of channels';

-- Mentions of a user in channel messages, written with the message by the
-- channel's transaction; read is set when the user reads the mention.
CREATE TABLE IF NOT EXISTS `channel_message_mentions` (
  `channel_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `message_id` int(11) NOT NULL,
  `read` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`channel_id`, `user_id`, `message_id`),
  KEY `channel_message` (`channel_id`, `message_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Mentions of users in channel messages';
