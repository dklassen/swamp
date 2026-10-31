package tui

import tea "charm.land/bubbletea/v2"

// reading keeps a model to one read in flight: asked again while one is,
// however many times, it reads once more when that one is done, since a
// sync elsewhere can log hundreds of writes.
type reading struct {
	inFlight, again bool
}

// start is read's command, or nil while a read is in flight.
func (r *reading) start(read func() tea.Cmd) tea.Cmd {
	if r.inFlight {
		r.again = true
		return nil
	}
	cmd := read()
	r.inFlight = cmd != nil
	return cmd
}

// done ends the read in flight, its result or its error, and starts the
// next if one was asked for meanwhile.
func (r *reading) done(read func() tea.Cmd) tea.Cmd {
	r.inFlight = false
	if !r.again {
		return nil
	}
	r.again = false
	return r.start(read)
}
