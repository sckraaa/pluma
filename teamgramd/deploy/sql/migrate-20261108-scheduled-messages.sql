-- Scheduled messages (task 061).
--
-- scheduled_messages holds the messages waiting in a schedule queue until
-- their time (core.telegram.org/api/scheduled-messages): one row per
-- message. id is the message's id in the queue, the one the clients see;
-- ids of different queues do not clash, and clients treat them as opaque.
--
-- queue_user_id, peer_type, peer_id name the queue: the sender's queue of a
-- dialog (queue_user_id is the sender), or, for a broadcast channel, the
-- queue shared by its admins (queue_user_id 0, peer the channel).
-- schedule_date is the time the message goes out, 2147483646 (0x7FFFFFFE)
-- when it waits for the peer to come online. message_data is the message as
-- it will go out, in JSON, as in messages. grouped_id is its album, 0 for
-- none. out_random_id keeps a client's random_id unique per sender, while
-- the message waits, as messages.out_random_id does once it is sent.
-- claim_until is the lease of a delivery worker (unix seconds): a row is
-- free when it is in the past.
--
-- A new table only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `scheduled_messages` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `queue_user_id` bigint(20) NOT NULL,
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `sender_user_id` bigint(20) NOT NULL,
  `random_id` bigint(20) NOT NULL DEFAULT '0',
  `out_random_id` bigint(20) GENERATED ALWAYS AS (IF(`random_id` <> 0, `random_id`, NULL)) VIRTUAL,
  `grouped_id` bigint(20) NOT NULL DEFAULT '0',
  `schedule_date` int(11) NOT NULL,
  `message_data` mediumtext COLLATE utf8mb4_bin NOT NULL,
  `claim_until` bigint(20) NOT NULL DEFAULT '0',
  `created_at` bigint(20) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `sender_out_random` (`sender_user_id`, `out_random_id`),
  KEY `queue_date` (`queue_user_id`, `peer_type`, `peer_id`, `schedule_date`),
  KEY `due` (`schedule_date`, `claim_until`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Messages waiting in a schedule queue';
