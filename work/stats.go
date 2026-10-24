package work

// VerifiedCount returns the number of verified shares held.
func (sc *ShareChain) VerifiedCount() int {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()
	n := 0
	for h := range sc.shares {
		if sc.verified[h] {
			n++
		}
	}
	return n
}
