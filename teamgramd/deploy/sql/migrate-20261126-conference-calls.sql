-- Conference calls (task 117, ADR 0075): a group call without a chat, in the
-- tables of group calls (migrate-20261117-group-calls.sql) and with their
-- end-to-end encryption (migrate-20261118-group-call-chain.sql).
--
--   group_calls.conference         1 for a conference. Its row has peer_type 0
--                                  and peer_id the id of the call, so that the
--                                  key live_peer holds as it is.
--   group_call_conferences         what a conference has besides the call: its
--                                  creator and the random_id of the request
--                                  that created it (a repeat gets the same
--                                  call), the slug of its link
--                                  (https://<t.me>/call/<slug>, 16 random bytes
--                                  in base64url) and since when no one is in
--                                  it (unix seconds, 0 while someone is).
--   group_call_invites             the invitations of a conference: a service
--                                  message messageActionConferenceCall in the
--                                  private chat of the inviter and the invitee
--                                  (dialog_message_id names it in both boxes).
--                                  state: 0 ringing, 1 the invitee joined, 2
--                                  missed (declined, cancelled, not answered),
--                                  3 the conference ended after he joined
--                                  (duration). shown is the state the message
--                                  shows: the collector edits the message
--                                  again while the two differ.
--   group_call_participants.kicked 1 for one the creator took out of a
--                                  conference: he does not come back by the
--                                  link or by another participant's invite.
--   phone_calls.discard_slug       the slug of the conference a one-to-one
--                                  call moved to (discard_reason 5).
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'group_calls' AND COLUMN_NAME = 'conference') = 0,
  'ALTER TABLE `group_calls` ADD COLUMN `conference` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'group_call_participants' AND COLUMN_NAME = 'kicked') = 0,
  'ALTER TABLE `group_call_participants` ADD COLUMN `kicked` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'phone_calls' AND COLUMN_NAME = 'discard_slug') = 0,
  'ALTER TABLE `phone_calls` ADD COLUMN `discard_slug` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '''', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `group_call_conferences` (
  `call_id` bigint(20) NOT NULL,
  `creator_id` bigint(20) NOT NULL,
  `random_id` int(11) NOT NULL,
  `slug` varchar(32) COLLATE utf8mb4_bin NOT NULL,
  `empty_since` int(11) NOT NULL DEFAULT '0',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`call_id`),
  UNIQUE KEY `slug` (`slug`),
  UNIQUE KEY `creator_random` (`creator_id`, `random_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Conference calls: group calls without a chat';

CREATE TABLE IF NOT EXISTS `group_call_invites` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `call_id` bigint(20) NOT NULL,
  `inviter_id` bigint(20) NOT NULL,
  `invitee_id` bigint(20) NOT NULL,
  `inviter_msg_id` int(11) NOT NULL,
  `dialog_message_id` bigint(20) NOT NULL,
  `video` tinyint(1) NOT NULL DEFAULT '0',
  `state` tinyint(4) NOT NULL DEFAULT '0',
  `shown` tinyint(4) NOT NULL DEFAULT '0',
  `duration` int(11) NOT NULL DEFAULT '0',
  `date` int(11) NOT NULL,
  `ring_until` int(11) NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `dialog_message` (`dialog_message_id`),
  KEY `call_state` (`call_id`, `state`),
  KEY `call_invitee` (`call_id`, `invitee_id`),
  KEY `ringing` (`state`, `ring_until`),
  KEY `inviter_date` (`inviter_id`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Invitations to conference calls';
