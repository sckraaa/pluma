-- The ids of channels come from the table itself (task 038): AUTO_INCREMENT
-- from 1000000000, so that they look like Telegram's and never meet the ids
-- of basic groups in a log. Snowflake ids of idgen do not fit: they pass
-- 2^53, the most Web K reads as a number. A row inserted with its own id
-- keeps it.
--
-- Safe on an empty and on a populated database, and safe to run again: the
-- counter only ever goes up.

ALTER TABLE `channels` MODIFY `id` bigint(20) NOT NULL AUTO_INCREMENT, AUTO_INCREMENT = 1000000000;
