-- Task 098, part G: messages.getRecentLocations selects the live locations
-- of a chat by message_filter_type = 14 (mtproto.MEDIA_GEO_LIVE). Messages
-- stored before it have -1 (MEDIA_EMPTY) there: this sets 14 on the rows
-- whose media is messageMediaGeoLive. msg stores the message as JSON
-- (jsonx), the constructor of the media under $.media.predicate_name, as
-- pkg/msgsearch reads it. Running the file again changes nothing: the
-- UPDATEs skip the rows that already have 14 and the keys are added once.

UPDATE `messages` SET `message_filter_type` = 14
WHERE `message_filter_type` <> 14
  AND JSON_UNQUOTE(JSON_EXTRACT(`message_data`, '$.media.predicate_name')) = 'messageMediaGeoLive';

UPDATE `channel_messages` SET `message_filter_type` = 14
WHERE `message_filter_type` <> 14
  AND JSON_UNQUOTE(JSON_EXTRACT(`message_data`, '$.media.predicate_name')) = 'messageMediaGeoLive';

-- The media lists of a dialog of a box (message.searchByMediaType: one
-- type, newest first) read the index instead of every message of the
-- dialog. Online DDL.
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'messages' AND INDEX_NAME = 'user_dialog_filter') = 0,
  'ALTER TABLE `messages` ADD KEY `user_dialog_filter` (`user_id`, `dialog_id1`, `dialog_id2`, `message_filter_type`, `user_message_box_id`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

-- The same for a channel (channel.search by a media type, newest first).
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_messages' AND INDEX_NAME = 'filter') = 0,
  'ALTER TABLE `channel_messages` ADD KEY `filter` (`channel_id`, `message_filter_type`, `id`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
