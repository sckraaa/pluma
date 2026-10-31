-- The suggestions a user dismissed with help.dismissSuggestion (task 020),
-- which help.getPromoData lists in dismissed_suggestions
-- (core.telegram.org/api/config, "Suggestions"). date2 is the time of the
-- dismissal: the birthday banner of contacts (BIRTHDAY_CONTACTS_TODAY) is
-- dismissed for a day, the others for good.
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `user_dismissed_suggestions` (
  `user_id` bigint(20) NOT NULL,
  `suggestion` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `date2` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `suggestion`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
