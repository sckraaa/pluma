-- Secret chats (task 069).
--
-- encrypted_chats: one row per secret chat. admin_* is the side that
-- requested it, participant_* the side that accepted; a chat belongs to one
-- authorization of each side (participant_auth_key_id is 0 until it is
-- accepted). The server keeps only the public Diffie-Hellman values, cleared
-- when the chat is discarded. state: 1 requested, 2 active, 3 discarded.
-- *_read_date: the last max_date of messages.readEncryptedHistory.
--
-- encrypted_chat_devices: per authorization (a key and the account signed in
-- on it: Android keeps the key across a logout), the last qts of its queue and
-- the time it was last given a state (the cursor of the updates that have no
-- qts).
--
-- encrypted_chat_queue: what an authorization has not confirmed yet
-- (messages.receivedQueue or the qts of updates.getDifference); data is the
-- client's ciphertext. encrypted_chat_sent keeps what messages.sendEncrypted*
-- answered, by random_id, for repeated requests. Both are kept 7 days at most
-- (core.telegram.org/api/end-to-end).
--
-- encrypted_files.encrypted_file_id: the id a download names.
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `encrypted_chats` (
  `id` int(11) NOT NULL,
  `access_hash` bigint(20) NOT NULL,
  `admin_id` bigint(20) NOT NULL,
  `admin_auth_key_id` bigint(20) NOT NULL,
  `participant_id` bigint(20) NOT NULL,
  `participant_auth_key_id` bigint(20) NOT NULL DEFAULT '0',
  `random_id` int(11) NOT NULL,
  `g_a` varbinary(256) NOT NULL,
  `g_b` varbinary(256) NOT NULL DEFAULT '',
  `key_fingerprint` bigint(20) NOT NULL DEFAULT '0',
  `state` tinyint(4) NOT NULL,
  `history_deleted` tinyint(1) NOT NULL DEFAULT '0',
  `discarded_by` bigint(20) NOT NULL DEFAULT '0',
  `date` int(11) NOT NULL,
  `state_at` int(11) NOT NULL,
  `admin_read_date` int(11) NOT NULL DEFAULT '0',
  `admin_read_at` int(11) NOT NULL DEFAULT '0',
  `participant_read_date` int(11) NOT NULL DEFAULT '0',
  `participant_read_at` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `request` (`admin_auth_key_id`, `random_id`),
  KEY `requester` (`admin_id`, `date`),
  KEY `participant` (`participant_id`, `state`),
  KEY `participant_device` (`participant_auth_key_id`),
  KEY `state` (`state`, `state_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `encrypted_chat_devices` (
  `auth_key_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `qts` int(11) NOT NULL DEFAULT '0',
  `state_given_at` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`auth_key_id`, `user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `encrypted_chat_queue` (
  `auth_key_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `qts` int(11) NOT NULL,
  `chat_id` int(11) NOT NULL,
  `random_id` bigint(20) NOT NULL,
  `date` int(11) NOT NULL,
  `service` tinyint(1) NOT NULL DEFAULT '0',
  `data` mediumblob NOT NULL,
  `data_size` int(11) NOT NULL DEFAULT '0',
  `file_id` bigint(20) NOT NULL DEFAULT '0',
  `file_access_hash` bigint(20) NOT NULL DEFAULT '0',
  `file_size` bigint(20) NOT NULL DEFAULT '0',
  `file_dc_id` int(11) NOT NULL DEFAULT '0',
  `file_key_fingerprint` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`auth_key_id`, `user_id`, `qts`),
  KEY `chat_id` (`chat_id`),
  KEY `date` (`date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `encrypted_chat_sent` (
  `chat_id` int(11) NOT NULL,
  `sender_auth_key_id` bigint(20) NOT NULL,
  `random_id` bigint(20) NOT NULL,
  `date` int(11) NOT NULL,
  `file_id` bigint(20) NOT NULL DEFAULT '0',
  `file_access_hash` bigint(20) NOT NULL DEFAULT '0',
  `file_size` bigint(20) NOT NULL DEFAULT '0',
  `file_dc_id` int(11) NOT NULL DEFAULT '0',
  `file_key_fingerprint` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`chat_id`, `sender_auth_key_id`, `random_id`),
  KEY `date` (`date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'encrypted_files' AND INDEX_NAME = 'encrypted_file_id') = 0,
  'ALTER TABLE `encrypted_files` ADD UNIQUE KEY `encrypted_file_id` (`encrypted_file_id`)',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
