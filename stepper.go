package main

// Forward, fine-grained single-stepping. A tree-walk interpreter can't be
// paused mid-recursion by returning — so we run Eval in a goroutine and park
// it on a channel before each mutation. The goroutine's own call stack IS the
// saved continuation; :step just unblocks one mutation at a time. This steps
// *into* loops and blocks (granularity = one state change), which is what
// "following the data" needs. Backward stepping is the existing :undo.

// gate is called by Interp.do() before every mutation. Outside step mode it is
// a no-op; in step mode it announces the pending op and blocks for permission.
func (ip *Interp) gate(label string) {
	if !ip.stepping {
		return
	}
	ip.gateIn <- label // hand the controller the op we're about to apply
	<-ip.gateOut       // wait for :step
}

// StartStep launches a program parked at its first mutation. Eval runs in a
// goroutine that blocks in gate() the moment it tries to mutate state. We then
// wait for that first park (or immediate completion) so the controller always
// holds the *next* op as pending.
func (ip *Interp) StartStep(program Node) {
	ip.stepping = true
	ip.gateIn = make(chan string)
	ip.gateOut = make(chan struct{})
	ip.stepDone = make(chan error, 1)
	go func() {
		_, err := Eval(program, ip)
		ip.stepping = false
		ip.stepDone <- err
	}()
	ip.await()
}

// await blocks until the evaluator has parked at its next mutation or finished,
// recording which. Because it only returns at a synchronization point, the
// goroutine is guaranteed idle afterward — the controller then has exclusive
// access to interp state (no data race with the parked evaluator).
func (ip *Interp) await() {
	select {
	case label := <-ip.gateIn:
		ip.stepPending = label
		ip.stepHasPending = true
	case e := <-ip.stepDone:
		ip.stepHasPending = false
		ip.stepErr = e
	}
}

// Step runs the one pending mutation, then waits for the evaluator to re-park
// (so the mutation is fully applied before returning). Reports finished=true
// only once the program has drained.
func (ip *Interp) Step() (executed string, finished bool, err error) {
	if !ip.stepHasPending {
		return "", true, ip.stepErr
	}
	ran := ip.stepPending
	ip.gateOut <- struct{}{} // permit the pending op; its redo runs now
	ip.await()               // ...and block until that redo completes
	return ran, false, nil
}
