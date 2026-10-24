-- Folders (task 013): every query of dialog_filters reads from_suggested,
-- the suggested folder a folder was made from, which no earlier script
-- creates; without it the folders of a user cannot be read. -1: not made
-- from a suggestion.
--
-- Safe on an empty and on a populated database, and safe to run again: the
-- column is added only when it is missing.
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'dialog_filters' AND COLUMN_NAME = 'from_suggested') = 0,
  'ALTER TABLE `dialog_filters` ADD `from_suggested` int(11) NOT NULL DEFAULT ''-1'' AFTER `order_value`, ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
