package session

// Host describes the machine the sessions run on.
type Host struct {
	Name   string
	Model  string
	Chip   string
	Memory uint64
	OS     string
	User   string
	Home   string
	// Portable tells a machine with a battery, as a laptop, where the
	// model does not.
	Portable bool
}
