-- Task 098, part D: what the clients report of how messages are read
-- (messages.reportReadMetrics) and how music is listened to
-- (messages.reportMusicListen), kept for the statistics of the chats
-- (task 103; pkg/readmetrics). The statements are CREATE TABLE IF NOT
-- EXISTS and nothing else: running the file again changes nothing.

-- One row per showing of a message on a reader's screen (view_id is the
-- client's id of the showing); a showing reported again is kept once
-- (INSERT IGNORE). The chat is as the reader names it: in a private chat
-- and a basic group msg_id is of the reader's own box, in a channel it is
-- the channel's. Leaving a channel keeps the rows, as channel_message_views
-- keeps the views; deleting the account removes them (idx_user_date).
CREATE TABLE IF NOT EXISTS `message_read_metrics` (
  `user_id` bigint NOT NULL COMMENT 'the reader',
  `peer_type` tinyint NOT NULL COMMENT 'PEER_USER, PEER_CHAT or PEER_CHANNEL',
  `peer_id` bigint NOT NULL,
  `msg_id` int NOT NULL,
  `view_id` bigint NOT NULL COMMENT 'inputMessageReadMetric.view_id',
  `time_in_view_ms` int NOT NULL,
  `active_time_in_view_ms` int NOT NULL,
  `height_to_viewport_ratio_permille` int NOT NULL COMMENT 'over 1000 for a message taller than the screen',
  `seen_range_ratio_permille` int NOT NULL COMMENT '0..1000',
  `date` bigint NOT NULL COMMENT 'unix seconds, when it was reported',
  PRIMARY KEY (`peer_type`, `peer_id`, `msg_id`, `user_id`, `view_id`),
  KEY `idx_user_date` (`user_id`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- One row per listener, document and calendar minute (minute = date div
-- 60), with the longest listened_duration of the listens of that minute.
CREATE TABLE IF NOT EXISTS `music_listens` (
  `user_id` bigint NOT NULL COMMENT 'the listener',
  `document_id` bigint NOT NULL,
  `minute` bigint NOT NULL COMMENT 'date div 60',
  `listened_duration` int NOT NULL COMMENT 'seconds, as the client reports',
  `date` bigint NOT NULL COMMENT 'unix seconds of the first listen of the minute',
  PRIMARY KEY (`user_id`, `document_id`, `minute`),
  KEY `idx_document_date` (`document_id`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
