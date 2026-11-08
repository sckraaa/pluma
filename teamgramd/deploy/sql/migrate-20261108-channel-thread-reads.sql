-- How far others read a thread (task 048).
--
-- messages.getDiscussionMessage answers read_outbox_max_id: how far the
-- readers other than the asker read the thread. The primary key of
-- channel_thread_reads starts with the reader, so the readers of one thread
-- need an index of their own.
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_thread_reads' AND INDEX_NAME = 'thread') = 0,
  'ALTER TABLE `channel_thread_reads` ADD KEY `thread` (`channel_id`, `top_id`, `read_max_id`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
