-- When a reader read a user's messages (task 020): msg keeps a row of
-- message_read_outbox per read in the sender's box, in private chats and
-- basic groups, for messages.getOutboxReadDate and
-- messages.getMessageReadParticipants. A row holds the range of the
-- sender's messages read with it: read_outbox_min_id to read_outbox_max_id.
-- Rows written before this version have 0, a range from the first message:
-- in private chats every read was kept, so the first row at or above a
-- message is still the one that read it.
--
-- Safe on an empty and on a populated database, and safe to run again: the
-- ALTER runs only when the column is missing.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'message_read_outbox' AND COLUMN_NAME = 'read_outbox_min_id') = 0,
  'ALTER TABLE `message_read_outbox` ADD COLUMN `read_outbox_min_id` int(11) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
