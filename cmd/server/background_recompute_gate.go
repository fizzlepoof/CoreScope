package main

import "log"

func (s *PacketStore) initBackgroundRecomputeGate() {
	s.backgroundRecomputeGateOnce.Do(func() {
		s.backgroundRecomputeGate = make(chan struct{}, 1)
	})
}

// runBackgroundRecompute waits for the gate. It is used for mandatory startup
// warmups, where every cache must be populated before Start returns.
func (s *PacketStore) runBackgroundRecompute(compute func() interface{}) interface{} {
	if compute == nil {
		return nil
	}
	s.initBackgroundRecomputeGate()
	s.backgroundRecomputeGate <- struct{}{}
	defer func() { <-s.backgroundRecomputeGate }()
	return compute()
}

// tryBackgroundRecompute runs one allocation-heavy background refresh at a
// time. A tick that arrives while another refresh is active is skipped rather
// than queued: cached snapshots remain valid, and the next periodic tick will
// retry without building an unbounded backlog.
func (s *PacketStore) tryBackgroundRecompute(name string, compute func() interface{}) (interface{}, bool) {
	if compute == nil {
		return nil, false
	}
	s.initBackgroundRecomputeGate()
	select {
	case s.backgroundRecomputeGate <- struct{}{}:
		defer func() { <-s.backgroundRecomputeGate }()
		return compute(), true
	default:
		skips := s.backgroundRecomputeSkips.Add(1)
		log.Printf("[recompute] skipped %s refresh while another background refresh is active (total_skips=%d)", name, skips)
		return nil, false
	}
}

func (s *PacketStore) BackgroundRecomputeSkips() int64 {
	return s.backgroundRecomputeSkips.Load()
}
