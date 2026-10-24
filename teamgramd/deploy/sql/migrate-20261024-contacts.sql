-- Contacts by phone number (task 011, ADR 0015): the phone book a user
-- imports is kept on the server, a limit bounds the new numbers a user
-- imports a day, the sign-up of an imported number is queued with the
-- account until its importers are told, and top peers keep the resets of
-- their ratings.
--
-- Safe on an empty and on a populated database, and safe to run again:
-- the tables are created once, the DELETE finds nothing the second time,
-- and the index is added only when it is missing.

-- The window of the import limit of each user: when it started (unix
-- seconds) and how many new numbers the user imported in it.
CREATE TABLE IF NOT EXISTS `contact_import_quota` (
  `user_id` bigint(20) NOT NULL,
  `window_start` bigint(20) NOT NULL,
  `imported` int(11) NOT NULL DEFAULT '0',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Import limit of contacts.importContacts';

-- Sign-ups not yet delivered to the users who imported the number: a row
-- is written with the account and deleted by the box transaction of the
-- new user that delivers the sign-up. created_at is unix seconds.
CREATE TABLE IF NOT EXISTS `contact_signups` (
  `user_id` bigint(20) NOT NULL,
  `phone` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `created_at` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`),
  KEY `created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Sign-ups of imported numbers until delivered';

-- When a user reset the top peer rating of a peer in a category
-- (contacts.resetTopPeerRating): only later messages count for it.
-- reset_at is unix seconds.
CREATE TABLE IF NOT EXISTS `user_top_peer_resets` (
  `user_id` bigint(20) NOT NULL,
  `category` tinyint(4) NOT NULL,
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `reset_at` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `category`, `peer_type`, `peer_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Resets of top peer ratings';

-- unregistered_contacts is the phone book of each user on the server: the
-- imported numbers that are not the user's contacts. A number registered
-- later becomes a contact and leaves it; the rows the previous version
-- marked imported = 1 at a sign-up are such numbers.
DELETE FROM `unregistered_contacts` WHERE `imported` = 1;

-- A user's phone book is read and reset by the user (contacts.getSaved,
-- contacts.resetSaved).
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'unregistered_contacts' AND INDEX_NAME = 'importer_user_id') = 0,
  'ALTER TABLE `unregistered_contacts` ADD KEY `importer_user_id` (`importer_user_id`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
