-- The names of the built-in inline bots (task 111): @gif (1000101) is
-- "GIF Search" and @pic (1000102) "Image Search", as the registry of
-- pkg/inlinebots has them, in place of "GIF" and "Pic" of
-- z_pluma-inline-bots.sql. The names are not translated: the user object is
-- the same for every viewer (ADR 0062, p. 6). Safe to run again.
--
-- This file sorts after z_pluma-inline-bots.sql ("names" after "bots"), so a
-- new database gets the accounts first and this renames them, as on an
-- existing one; a migrate-*.sql file would run before the accounts exist. An
-- account a person or a channel kept from being made (its username was
-- taken) stays absent: only bot accounts with these ids are renamed.
--
-- The cached users stay in Redis until they expire or are deleted, and
-- clients learn the new name when they next load the user; pluma-admin
-- inline-bots photos drops the cache and tells the clients
-- (docs/ops/inline-bots.md).
UPDATE users SET first_name = 'GIF Search', last_name = ''
WHERE id = 1000101 AND is_bot = 1 AND (first_name <> 'GIF Search' OR last_name <> '');

UPDATE users SET first_name = 'Image Search', last_name = ''
WHERE id = 1000102 AND is_bot = 1 AND (first_name <> 'Image Search' OR last_name <> '');
