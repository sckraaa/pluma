-- The quota of channels.searchPosts (task 066): one row per different query
-- a user ran in a UTC day (`day` = unix time / 86400); query_hash is a hash
-- of the query, the text itself is not kept. Rows of past days go on the
-- next search of the same user.
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `posts_search_slots` (
  `user_id` bigint(20) NOT NULL,
  `day` int(11) NOT NULL,
  `query_hash` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `day`, `query_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Queries of channels.searchPosts per user and day';
