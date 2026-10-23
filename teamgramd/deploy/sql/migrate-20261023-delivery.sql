-- Reliable delivery (task 003, ADR 0012): the events of a box transaction
-- are rows of mq_outbox written by that transaction; a relay publishes them
-- to Kafka in id order, and the recipient's transaction that applies an
-- inbox event deletes its row. The messages of a box get two unique keys:
-- one copy of a message per user, one outgoing message per random_id.
--
-- Safe on an empty and on a populated database, and safe to run again: the
-- table is created once, the UPDATEs find nothing the second time, and each
-- ALTER runs only when what it adds is missing.

-- One row per event (ADR 0012, p. 1). The id is never reused: a copy in
-- Kafka of an applied event finds no row. state: 0 pending, 1 published,
-- 2 dead, 3 parked. Times are unix milliseconds.
CREATE TABLE IF NOT EXISTS `mq_outbox` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `initiator_id` bigint(20) NOT NULL,
  `topic` varchar(64) NOT NULL,
  `recipient_id` bigint(20) NOT NULL,
  `method` varchar(128) NOT NULL,
  `payload` mediumblob NOT NULL,
  `trace` varchar(64) NOT NULL DEFAULT '',
  `state` tinyint(4) NOT NULL DEFAULT '0',
  `revision` int(11) NOT NULL DEFAULT '0',
  `publish_count` int(11) NOT NULL DEFAULT '0',
  `kafka_partition` int(11) NOT NULL DEFAULT '-1',
  `kafka_offset` bigint(20) NOT NULL DEFAULT '-1',
  `redeliveries` int(11) NOT NULL DEFAULT '0',
  `deferrals` int(11) NOT NULL DEFAULT '0',
  `attempts` int(11) NOT NULL DEFAULT '0',
  `replays` int(11) NOT NULL DEFAULT '0',
  `due_at` bigint(20) NOT NULL DEFAULT '0',
  `error` varchar(512) NOT NULL DEFAULT '',
  `created_at` bigint(20) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `state` (`state`, `id`),
  KEY `recipient` (`topic`, `recipient_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Events of box transactions until applied or published';

-- Copies without a link to the sender's message: dialog_message_id 0 (an
-- idgen failure before task 002). -id is unique and never meets a
-- snowflake id, which is positive.
UPDATE `messages` SET `dialog_message_id` = -`id` WHERE `dialog_message_id` = 0;

-- Repeated copies of one message in one box (an event delivered twice): the
-- first copy stays linked, the others are unlinked. Clients that showed
-- them keep them; the sender's edits and deletions reach the first.
UPDATE `messages` m JOIN (
    SELECT `user_id`, `dialog_message_id`, MIN(`user_message_box_id`) AS `keep_id`
    FROM `messages` WHERE `dialog_message_id` > 0
    GROUP BY `user_id`, `dialog_message_id` HAVING COUNT(*) > 1) d
  ON m.`user_id` = d.`user_id` AND m.`dialog_message_id` = d.`dialog_message_id` AND m.`user_message_box_id` <> d.`keep_id`
SET m.`dialog_message_id` = -m.`id`;

-- Repeated outgoing messages with one random_id (a client repeating a send
-- after the Redis window): the first keeps it.
UPDATE `messages` m JOIN (
    SELECT `user_id`, `random_id`, MIN(`user_message_box_id`) AS `keep_id`
    FROM `messages` WHERE `sender_user_id` = `user_id` AND `random_id` <> 0
    GROUP BY `user_id`, `random_id` HAVING COUNT(*) > 1) d
  ON m.`user_id` = d.`user_id` AND m.`random_id` = d.`random_id` AND m.`sender_user_id` = m.`user_id` AND m.`user_message_box_id` <> d.`keep_id`
SET m.`random_id` = 0;

-- pts of the journal row that added the copy to the box (0 for copies
-- written before this version): a repeated send answers with it.
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'messages' AND COLUMN_NAME = 'pts') = 0,
  'ALTER TABLE `messages` ADD COLUMN `pts` int(11) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

-- The random_id of the sender's own copy, NULL for every other row (the
-- recipients' copies carry the sender's random_id, service messages 0): a
-- NULL never conflicts in a unique key.
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'messages' AND COLUMN_NAME = 'out_random_id') = 0,
  'ALTER TABLE `messages` ADD COLUMN `out_random_id` bigint(20) GENERATED ALWAYS AS (IF(`sender_user_id` = `user_id` AND `random_id` <> 0, `random_id`, NULL)) VIRTUAL',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

-- One copy of a message per box; also the index of the lookups of a copy
-- by dialog_message_id (edit, delete, read, pin). Online DDL.
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'messages' AND INDEX_NAME = 'user_dialog_message') = 0,
  'ALTER TABLE `messages` ADD UNIQUE KEY `user_dialog_message` (`user_id`, `dialog_message_id`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

-- One outgoing message per random_id of a user, for good (no window).
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'messages' AND INDEX_NAME = 'user_out_random') = 0,
  'ALTER TABLE `messages` ADD UNIQUE KEY `user_out_random` (`user_id`, `out_random_id`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
