-- Durable box sequences (task 002, ADR 0011): the counters of a user's
-- common message box (pts, user_message_box_id; qts and seq later) live in
-- MySQL, in one row per user, and change only in the transaction that writes
-- the data and the journal rows they describe (pkg/userbox). Redis no longer
-- holds them.
--
-- Safe to run again: it creates what is missing and never moves an existing
-- user_state row (raising rows is `pluma-admin state-raise`, ADR 0011 p. 11).

-- No row means the base state: pts 1, no box ids issued. The first write to
-- a box creates its row.
CREATE TABLE IF NOT EXISTS `user_state` (
  `user_id` bigint(20) NOT NULL,
  `pts` int(11) NOT NULL DEFAULT '1',
  `qts` int(11) NOT NULL DEFAULT '0',
  `seq` int(11) NOT NULL DEFAULT '0',
  `last_message_box_id` int(11) NOT NULL DEFAULT '0',
  `date2` bigint(20) NOT NULL DEFAULT '0',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Committed state of each user''s common message box';

-- The journal holds one row per pts. Duplicates left by counter resets in
-- Redis: keep the newest row of each (user_id, pts).
DELETE p1 FROM `user_pts_updates` p1
  JOIN `user_pts_updates` p2 ON p2.`user_id` = p1.`user_id` AND p2.`pts` = p1.`pts` AND p2.`id` > p1.`id`;
ALTER TABLE `user_pts_updates` DROP INDEX `user_id`, ADD UNIQUE KEY `user_id` (`user_id`, `pts`);

-- Every existing user starts above anything a client may have seen: the
-- Redis counters ran ahead of the journal and of the messages (pts burned
-- without a journal row, ids returned before the row was written), by an
-- unknown amount. A jump costs a device one updates.getDifference; a step
-- back would make clients drop new updates as already applied.
-- Only missing rows are added, so running this again changes nothing.
INSERT INTO `user_state` (`user_id`, `pts`, `last_message_box_id`, `date2`)
SELECT * FROM (
  SELECT u.`id` AS `user_id`,
         GREATEST(1, COALESCE(p.`max_pts`, 0)) + 1000000 AS `pts`,
         COALESCE(m.`max_box_id`, 0) + 1000000 AS `last_message_box_id`,
         UNIX_TIMESTAMP() AS `date2`
  FROM (SELECT `id` FROM `users`
        UNION SELECT `user_id` FROM `user_pts_updates`
        UNION SELECT `user_id` FROM `messages`) u
  LEFT JOIN (SELECT `user_id`, MAX(`pts`) AS `max_pts` FROM `user_pts_updates` GROUP BY `user_id`) p ON p.`user_id` = u.`id`
  LEFT JOIN (SELECT `user_id`, MAX(`user_message_box_id`) AS `max_box_id` FROM `messages` GROUP BY `user_id`) m ON m.`user_id` = u.`id`
) s
ON DUPLICATE KEY UPDATE `user_id` = `user_state`.`user_id`;
