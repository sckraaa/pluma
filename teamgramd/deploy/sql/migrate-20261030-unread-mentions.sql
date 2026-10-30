-- The unread mention counter of a basic group dialog counts the incoming
-- messages that mention the user and still have the unread media flag, as
-- the clients count unread mentions (task 020). Before, the pin of the
-- user's own message, which mentions them without that flag, was counted
-- too, and no client could read it off. The counters are recounted from the
-- messages; mentions exist only in basic groups (peer_type 3, dialog_id1 -3).
-- The dialog cache key version went up with this change (dialog.2), so no
-- old counter is read from Redis.
--
-- Safe on an empty and on a populated database, and safe to run again.

UPDATE `dialogs` d SET d.`unread_mentions_count` = (
  SELECT COUNT(*) FROM `messages` m
  WHERE m.`user_id` = d.`user_id` AND m.`dialog_id1` = -3 AND m.`dialog_id2` = d.`peer_id`
    AND m.`mentioned` = 1 AND m.`media_unread` = 1 AND m.`sender_user_id` <> d.`user_id` AND m.`deleted` = 0)
WHERE d.`peer_type` = 3;
