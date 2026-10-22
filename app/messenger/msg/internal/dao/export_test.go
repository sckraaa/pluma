package dao

// IdempotentStore is the Redis side of the idempotency window, for tests.
type IdempotentStore = idempotentStore

// SetIdempotentStore makes d open the idempotency window of a send with f.
func (d *Dao) SetIdempotentStore(f func(key string) IdempotentStore) {
	d.idempotent = f
}
