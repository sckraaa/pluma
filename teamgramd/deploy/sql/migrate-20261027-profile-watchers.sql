-- Who sees a user's name and photo (task 010, ADR 0021): the users whose
-- contact the user is and the users with a private dialog with the user.
-- A change of the name, the username, the photo or Premium tells each of
-- them with seq, and the box transaction of the change reads them by the
-- other user: user_contacts by contact_user_id, dialogs by the peer. Without
-- these indexes each read would scan the whole table.
--
-- Online DDL: reads and writes of both tables go on while an index is built.
-- Safe on an empty and on a populated database, and safe to run again: an
-- index is added only when it is missing.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_contacts' AND INDEX_NAME = 'contact_user_id') = 0,
  'ALTER TABLE `user_contacts` ADD KEY `contact_user_id` (`contact_user_id`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'dialogs' AND INDEX_NAME = 'peer') = 0,
  'ALTER TABLE `dialogs` ADD KEY `peer` (`peer_type`, `peer_id`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
