-- The public photo and the personal photo of a contact (task 051, ADR 0033).
--
-- users.fallback_photo_id is the public photo of an account: shown to the
-- users the privacy of the profile photo hides the main photo from. It is
-- not one of the account's photos (user_profile_photos), as in Telegram,
-- whose clients add it to the gallery themselves. 0 is none.
--
-- user_contacts.personal_photo_id is the photo the owner of the row set for
-- the contact, seen by the owner alone (userProfilePhoto.personal), and
-- personal_photo_has_video tells whether it is a video. 0 is none; deleting
-- the contact removes it.
--
-- user_suggested_photos are the photos other users suggested to a user
-- (photos.uploadContactProfilePhoto with suggest): the user may make one of
-- them its main photo although another user uploaded it. date2 is unix
-- seconds.
--
-- Safe on an empty and on a populated database, and safe to run again: a
-- column is added only when it is missing, the table is created once.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'fallback_photo_id') = 0,
  'ALTER TABLE `users` ADD COLUMN `fallback_photo_id` bigint(20) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_contacts' AND COLUMN_NAME = 'personal_photo_id') = 0,
  'ALTER TABLE `user_contacts` ADD COLUMN `personal_photo_id` bigint(20) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_contacts' AND COLUMN_NAME = 'personal_photo_has_video') = 0,
  'ALTER TABLE `user_contacts` ADD COLUMN `personal_photo_has_video` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `user_suggested_photos` (
  `user_id` bigint(20) NOT NULL,
  `photo_id` bigint(20) NOT NULL,
  `from_user_id` bigint(20) NOT NULL,
  `date2` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `photo_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Profile photos suggested to a user';
