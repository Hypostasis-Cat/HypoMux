package services

// RecoverSystemProxy restores the user proxy snapshot captured before HypoMux
// took ownership. It is also used by the installer after an interrupted UI
// session, before application files are replaced or removed.
func RecoverSystemProxy() error {
	return restoreSystemProxy()
}
