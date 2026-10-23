package dialog

import (
	"time"

	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// CacheTimeout limits a cache deletion around a box transaction of dialog.
const CacheTimeout = 5 * time.Second

// MaxBoxOpTime is the longest a box operation of dialog takes: the cache
// deletions before and after the transaction and the transaction itself
// (userbox.MaxRunTime). The RPC deadline of a method that runs one must
// outlive it (userbox.CheckRPCTimeout).
const MaxBoxOpTime = CacheTimeout + userbox.MaxRunTime + CacheTimeout
