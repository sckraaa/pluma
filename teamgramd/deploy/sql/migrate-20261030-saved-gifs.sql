-- The saved GIFs of an account (messages.saveGif, task 020): the newest
-- first by order2, at most saved_gifs_limit_default of them, or
-- saved_gifs_limit_premium with Premium (help.getAppConfig). Every change
-- goes in a box transaction of the account with updateSavedGifs (ADR 0020).
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `user_saved_gifs` (
  `user_id` bigint(20) NOT NULL,
  `document_id` bigint(20) NOT NULL,
  `order2` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `document_id`),
  KEY `user_order` (`user_id`, `order2`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
