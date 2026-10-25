-- Removal of auth keys (task 008): authsession removes expired temporary
-- keys every 10 minutes, and the temporary keys of a permanent key on
-- destroy_auth_key and auth.dropTempAuthKeys; a removed temporary key is
-- also cleared from the permanent key that referred to it. These indexes
-- keep each of these from reading the whole table.
ALTER TABLE `auth_key_infos`
  ADD KEY `expires_at` (`expires_at`),
  ADD KEY `perm_auth_key_id` (`perm_auth_key_id`),
  ADD KEY `temp_auth_key_id` (`temp_auth_key_id`),
  ADD KEY `media_temp_auth_key_id` (`media_temp_auth_key_id`);
