-- The cards of links (task 023, ADR 0027): one row per normalized address,
-- the card (mtproto.WebPage, protobuf) and the time it is kept until, after
-- which the page is fetched again. webpage_id stays the same over refetches:
-- clients keep cards by id. url is for the operator; url_hash (SHA-256 of
-- it) is the key, as addresses are longer than an index allows.
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `webpages` (
  `url_hash` binary(32) NOT NULL,
  `url` varchar(2048) COLLATE utf8mb4_unicode_ci NOT NULL,
  `webpage_id` bigint(20) NOT NULL,
  `data` mediumblob NOT NULL,
  `fetched_at` bigint(20) NOT NULL DEFAULT '0',
  `expires_at` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`url_hash`),
  UNIQUE KEY `webpage_id` (`webpage_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
