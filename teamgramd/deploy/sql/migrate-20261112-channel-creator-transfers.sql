-- The owner of a supergroup or channel who leaves it (task 078,
-- core.telegram.org/api/channel, "Leaving groups/channels"): the member
-- channels.getFutureCreatorAfterLeave named becomes the owner seven days
-- later, unless the owner comes back first. A row is such a transfer
-- waiting: from_user_id left at date2, to_user_id takes the channel at
-- due_at. The row goes when the transfer is done or the owner comes back.
--
-- A new table only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `channel_creator_transfers` (
  `channel_id` bigint(20) NOT NULL,
  `from_user_id` bigint(20) NOT NULL,
  `to_user_id` bigint(20) NOT NULL,
  `due_at` bigint(20) NOT NULL,
  `date2` bigint(20) NOT NULL,
  PRIMARY KEY (`channel_id`),
  KEY `due_at` (`due_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
