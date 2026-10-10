package testutil

import "time"

// nowUnix returns the current Unix time in seconds, used to default ID-token
// iat/exp claims.
func nowUnix() int64 { return time.Now().Unix() }
