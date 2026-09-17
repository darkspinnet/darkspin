//go:build bindings

package window

// Acquire is inert while Wails inspects application bindings.
func Acquire(InstanceOptions) (func(), bool, error) {
	return func() {}, false, nil
}

// InstanceOptions is retained for binding-build compatibility.
type InstanceOptions struct {
	MutexName string
}
