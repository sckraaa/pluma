-- Task 090, ADR 0056: transcription of voice messages and round videos.
-- The statements are CREATE TABLE IF NOT EXISTS and nothing else: running
-- the file again changes nothing.

-- One row per document and model: every copy and forward of a message has
-- the same document, so a record is transcribed once for all its readers.
-- The table is the queue of the worker in bff as well (state, retry_at,
-- lease_until): it keeps every record asked for, of any length, and the
-- worker takes them in the order they were queued. The model is in the key: after STT_MODEL changes the old
-- texts stay and are not served, and a record is transcribed again by the
-- new model when somebody asks.
CREATE TABLE IF NOT EXISTS `transcription_jobs` (
  `document_id` bigint NOT NULL,
  `model` varchar(64) NOT NULL COMMENT 'STT_MODEL the text was made with',
  `access_hash` bigint NOT NULL DEFAULT 0 COMMENT 'of the document',
  `state` tinyint NOT NULL COMMENT 'pkg/transcribe: 1 queued, 2 running, 3 done, 4 failed',
  `text` mediumtext NULL COMMENT 'the text of a done job; empty: no speech',
  `permanent` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'a failed job that is never tried again: the record cannot be decoded',
  `attempts` int NOT NULL DEFAULT 0 COMMENT 'attempts started since the job was queued',
  `duration` int NOT NULL DEFAULT 0 COMMENT 'seconds, as the attribute of the document says: the client''s word, nothing is timed by it',
  `file_size` bigint NOT NULL DEFAULT 0 COMMENT 'bytes of the document',
  `lease_until` bigint NOT NULL DEFAULT 0 COMMENT 'unix ms: a running attempt holds the job until then',
  `retry_at` bigint NOT NULL DEFAULT 0 COMMENT 'unix ms: a queued job is due from then',
  `queued_at` bigint NOT NULL DEFAULT 0 COMMENT 'unix ms: when it was queued, the last time',
  `finished_at` bigint NOT NULL DEFAULT 0 COMMENT 'unix ms: when it was done or failed',
  `created_at` bigint NOT NULL,
  PRIMARY KEY (`document_id`, `model`),
  KEY `idx_state_retry` (`state`, `retry_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- One row per reader and message. Its id is the transcription_id of
-- messages.transcribedAudio, updateTranscribedAudio and
-- messages.rateTranscribedAudio: the clients match an update and send a
-- rating by it, so it is never shared by two messages of one reader.
CREATE TABLE IF NOT EXISTS `transcription_requests` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `user_id` bigint NOT NULL COMMENT 'the reader',
  `peer_type` tinyint NOT NULL COMMENT 'the dialog as the reader sees it',
  `peer_id` bigint NOT NULL,
  `msg_id` int NOT NULL COMMENT 'of the copy of the reader',
  `document_id` bigint NOT NULL,
  `model` varchar(64) NOT NULL,
  `delivery` tinyint NOT NULL DEFAULT 0 COMMENT 'the final update: 0 not owed, 1 owed (the answer was pending), 2 sent',
  `deliver_after` bigint NOT NULL DEFAULT 0 COMMENT 'unix ms: the update goes not before',
  `rating` tinyint(1) NULL COMMENT 'messages.rateTranscribedAudio: 1 good, 0 bad',
  `created_at` bigint NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_reader_message` (`user_id`, `peer_type`, `peer_id`, `msg_id`),
  KEY `idx_delivery` (`delivery`, `deliver_after`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
