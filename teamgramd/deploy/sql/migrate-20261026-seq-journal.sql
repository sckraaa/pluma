-- The seq journal of each account (task 010, ADR 0020).
--
-- user_seq_updates holds one row per seq: the update with seq, as it was
-- pushed, written by the box transaction that changed what it describes
-- (pkg/userbox, under the lock of the user's user_state row). pts is the pts
-- of the box when the row was written: updates.getDifference puts the update
-- among the pts updates of the journal where it happened. The primary key
-- makes a second row of one seq fail its transaction, as UNIQUE (user_id,
-- pts) does for user_pts_updates.
--
-- user_seq_cursors holds, for each authorization of an account, the seq of
-- the last state updates.getState or a complete updates.getDifference gave
-- it: the difference also returns the updates after it, so an update with
-- seq whose push the device lost is found even when the device moved its
-- date with other updates (Telegram Desktop does).
--
-- auth_seq_updates (a seq journal by auth key) was never written and is no
-- longer read; it stays for a rollback to a version before task 010.
--
-- Safe to run again: the tables are created once.
CREATE TABLE IF NOT EXISTS `user_seq_updates` (
  `user_id` bigint(20) NOT NULL,
  `seq` int(11) NOT NULL,
  `pts` int(11) NOT NULL,
  `update_type` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `update_data` json NOT NULL,
  `date2` bigint(20) NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_id`, `seq`),
  KEY `user_date` (`user_id`, `date2`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Updates with seq of each account';

CREATE TABLE IF NOT EXISTS `user_seq_cursors` (
  `user_id` bigint(20) NOT NULL,
  `auth_key_id` bigint(20) NOT NULL,
  `seq` int(11) NOT NULL,
  `date2` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `auth_key_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='seq each authorization was last given a state of';
