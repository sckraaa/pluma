-- Quick replies of Telegram Business (task 105, ADR 0067).
--
-- quick_reply_shortcuts holds the shortcuts of an account
-- (core.telegram.org/api/business#quick-reply-shortcuts): id is the
-- shortcut_id the clients see; shortcut is the name as the owner wrote it;
-- shortcut_key is the name case-folded, so that two names differing only in
-- case are one shortcut, as tdesktop looks a name up (data_shortcut_messages
-- .cpp, lookupShortcutId, Qt::CaseInsensitive). The key is compared byte by
-- byte: a collation would also take accents for the same letter, which the
-- clients do not. sort is the place of the shortcut in the account's order
-- (messages.reorderQuickReplies). A shortcut is deleted as a row, with its
-- messages, in one transaction.
--
-- quick_reply_messages holds the messages of the shortcuts: id is the
-- message id the clients see, a sequence of its own apart from the ids of
-- the account's message box ("a separate sequence", api/business). The
-- message is kept in message_data as it is answered, in JSON as in
-- messages, without its id, date and edit date, which are the columns.
-- grouped_id is its album, 0 for none. out_random_id keeps a client's
-- random_id unique per account, so that a send repeated after a lost
-- answer gets the first answer.
--
-- New tables only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `quick_reply_shortcuts` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `user_id` bigint(20) NOT NULL,
  `shortcut` varchar(64) NOT NULL,
  `shortcut_key` varchar(64) NOT NULL,
  `sort` int(11) NOT NULL,
  `created_at` bigint(20) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `user_shortcut` (`user_id`, `shortcut_key`),
  KEY `user_sort` (`user_id`, `sort`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Quick reply shortcuts of Telegram Business';

CREATE TABLE IF NOT EXISTS `quick_reply_messages` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `user_id` bigint(20) NOT NULL,
  `shortcut_id` int(11) NOT NULL,
  `random_id` bigint(20) NOT NULL DEFAULT '0',
  `out_random_id` bigint(20) GENERATED ALWAYS AS (IF(`random_id` <> 0, `random_id`, NULL)) VIRTUAL,
  `grouped_id` bigint(20) NOT NULL DEFAULT '0',
  `message_data` mediumtext NOT NULL,
  `date` int(11) NOT NULL,
  `edit_date` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `user_out_random` (`user_id`, `out_random_id`),
  KEY `shortcut` (`shortcut_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Messages of quick reply shortcuts';
