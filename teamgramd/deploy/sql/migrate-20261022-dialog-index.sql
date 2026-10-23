-- The messages index of a dialog gets the columns the box transactions
-- (task 002, ADR 0011) and the history reads select and order by:
-- deleted and user_message_box_id. The newest message of a dialog, its
-- unread range after read_inbox_max_id and a page of its history are then
-- read from the index instead of from every row of the dialog, which in a
-- long group chat held the user's box lock for the time of a scan on each
-- read and deletion.
--
-- The old index is a prefix of the new one and goes. Online DDL: reads and
-- writes of messages go on while the index is built. Safe to run again: it
-- builds the same index once more.
ALTER TABLE `messages`
  DROP INDEX `user_id`,
  ADD KEY `user_id` (`user_id`, `dialog_id1`, `dialog_id2`, `deleted`, `user_message_box_id`),
  ALGORITHM = INPLACE, LOCK = NONE;
