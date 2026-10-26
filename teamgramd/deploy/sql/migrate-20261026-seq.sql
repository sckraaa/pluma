-- The seq of an account starts at 1 (task 010, ADR 0019): the first update
-- with seq will get 2, which a client holding 1 applies without a gap.
-- updates.getState sent -1 for the stored 0 before; clients take the seq of
-- updates.getState and updates.getDifference as it is, and no update carries
-- seq yet, so the change needs nothing from them.
--
-- Safe to run again: only rows still at 0 change.
ALTER TABLE `user_state` ALTER `seq` SET DEFAULT 1;
UPDATE `user_state` SET `seq` = 1 WHERE `seq` = 0;
