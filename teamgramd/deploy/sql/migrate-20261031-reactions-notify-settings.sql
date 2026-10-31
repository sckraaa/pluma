-- The reaction notification settings of an account
-- (account.getReactionsNotifySettings, account.setReactionsNotifySettings,
-- task 020): one row per user, written whole by every change; an account
-- without a row has the defaults (contacts for all three, the sound
-- notificationSoundLocal "default", previews shown).
--
-- messages_notify_from, stories_notify_from and poll_votes_notify_from:
-- 0 nobody (the field absent), 1 reactionNotificationsFromContacts,
-- 2 reactionNotificationsFromAll.
-- sound_kind: 0 notificationSoundDefault, 1 notificationSoundNone,
-- 2 notificationSoundLocal (sound_title, sound_data),
-- 3 notificationSoundRingtone (sound_ringtone_id).
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `user_reactions_notify_settings` (
  `user_id` bigint(20) NOT NULL,
  `messages_notify_from` tinyint(4) NOT NULL,
  `stories_notify_from` tinyint(4) NOT NULL,
  `poll_votes_notify_from` tinyint(4) NOT NULL,
  `sound_kind` tinyint(4) NOT NULL,
  `sound_title` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `sound_data` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `sound_ringtone_id` bigint(20) NOT NULL,
  `show_previews` tinyint(1) NOT NULL,
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
