-- Sticker and emoji sets fetched from Telegram when a user asks for one
-- (task 030): kept beside the catalog's (source 'demand'), which an import
-- of the catalog replaces without touching them ('catalog').
--
-- Safe on an empty and on a populated database, and safe to run again: the
-- column is added only when it is missing.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'catalog_sticker_sets' AND COLUMN_NAME = 'source') = 0,
  'ALTER TABLE `catalog_sticker_sets` ADD `source` varchar(16) COLLATE utf8mb4_bin NOT NULL DEFAULT ''catalog'' AFTER `kind`',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
