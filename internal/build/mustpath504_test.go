package build

// mustPath returns a store path, failing loudly on an invalid name: test
// names are always valid (#504).
func mustPath(p string, err error) string {
	if err != nil {
		panic(err)
	}
	return p
}
