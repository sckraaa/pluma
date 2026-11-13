-- The blockchain of the end-to-end encryption of group calls (task 094,
-- ADR 0060 §2). Every group call is end-to-end encrypted: the clients write
-- the blocks, the server checks and orders them (pkg/tde2e) and never holds
-- a key.
--
--   group_calls.chain_height       height of the last block of sub-chain 0;
--                                  -1 before the first block (and after the
--                                  blocks of a call that is over are gone).
--   group_calls.chain_hash         SHA-256 of that block in the local form;
--                                  '' before the first block.
--   group_calls.broadcast_count    broadcasts of sub-chain 1 in the life of
--                                  the call: the index of the next one. It
--                                  is never reset while the call lasts.
--   group_call_chain_blocks        the blocks of both sub-chains in the local
--                                  form, as the clients sent them (the server
--                                  adds one to the magic when it hands one
--                                  out). sub_chain 0: idx is the height;
--                                  sub_chain 1: idx is the index of the
--                                  broadcast and height the chain_height it
--                                  names. The state the next block is checked
--                                  against is that of the last block of
--                                  sub-chain 0, which carries it.
--   group_call_chain_keys          the user each public key first entered a
--                                  call's chain with: a key belongs to that
--                                  user for the life of the call.
--   group_call_e2e_diag            what the server knows of the encryption of
--                                  each participant (polls, epochs, the last
--                                  status report of its client): prefixes of
--                                  hashes and counters, never a key. Read by
--                                  pluma-admin calls show.
-- The rows of a call go some time after it is over (the collector of bff).
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'group_calls' AND COLUMN_NAME = 'chain_height') = 0,
  'ALTER TABLE `group_calls` ADD COLUMN `chain_height` int(11) NOT NULL DEFAULT ''-1'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'group_calls' AND COLUMN_NAME = 'chain_hash') = 0,
  'ALTER TABLE `group_calls` ADD COLUMN `chain_hash` varbinary(32) NOT NULL DEFAULT '''', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'group_calls' AND COLUMN_NAME = 'broadcast_count') = 0,
  'ALTER TABLE `group_calls` ADD COLUMN `broadcast_count` int(11) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `group_call_chain_blocks` (
  `call_id` bigint(20) NOT NULL,
  `sub_chain` tinyint(4) NOT NULL,
  `idx` int(11) NOT NULL,
  `block` mediumblob NOT NULL,
  `hash` binary(32) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `height` int(11) NOT NULL,
  `date` int(11) NOT NULL,
  PRIMARY KEY (`call_id`, `sub_chain`, `idx`),
  KEY `call_height` (`call_id`, `sub_chain`, `height`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Blocks of the end-to-end encryption of group calls';

CREATE TABLE IF NOT EXISTS `group_call_chain_keys` (
  `call_id` bigint(20) NOT NULL,
  `public_key` binary(32) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  PRIMARY KEY (`call_id`, `public_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Whose each public key of a group call is';

CREATE TABLE IF NOT EXISTS `group_call_e2e_diag` (
  `call_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `updated_at` bigint(20) NOT NULL,
  `diag` text COLLATE utf8mb4_unicode_ci NOT NULL,
  PRIMARY KEY (`call_id`, `user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='What the server sees of the encryption of each participant of a group call';
