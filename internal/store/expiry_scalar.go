package store

// markExpiredScalar replaces each private deadline with -1 if expired, else 0.
func markExpiredScalar(deadlines []int64, now int64) {
	for i, d := range deadlines {
		var mask int64
		if d > 0 && now > d {
			mask = -1
		}
		deadlines[i] = mask
	}
}
