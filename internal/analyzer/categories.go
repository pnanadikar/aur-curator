package analyzer

// Finding categories. Every category an analyzer emits is one of these
// constants; TestCategoriesAreConstants fails on a string literal, and
// policy's TestEveryAnalyzerCategoryMapped fails if one is missing from
// the severity table (#475, risk-analysis.md § Unmapped categories).
//
// A category that has been in a release is part of the stored review
// format: findings are saved with it. Removing or renaming one needs a
// migration that remaps stored findings (DEVELOPMENT.md § Principles).
const (
	// CategoryURLNew is a new download location in source=().
	CategoryURLNew = "url_new"
	// CategoryPGPRemoved is PGP verification removed.
	CategoryPGPRemoved = "pgp_removed"
	// CategoryChecksumRemoved is checksum verification removed or
	// weakened.
	CategoryChecksumRemoved = "checksum_removed"
	// CategoryChecksumRefresh is checksums changed with the sources.
	CategoryChecksumRefresh = "checksum_refresh"
	// CategoryVersionBump is pkgver/pkgrel changed.
	CategoryVersionBump = "version_bump"
	// CategoryShellExec is new shell execution in the PKGBUILD or
	// .install.
	CategoryShellExec = "shell_exec"
	// CategoryNetworkDownload is network access from PKGBUILD or
	// .install code rather than through source=().
	CategoryNetworkDownload = "network_download"
	// CategoryInstallChanged is a .install scriptlet added or changed
	// (#476).
	CategoryInstallChanged = "install_changed"
	// CategoryPGPKeyChanged is a key added to validpgpkeys, with or
	// without one removed (#476).
	CategoryPGPKeyChanged = "pgp_key_changed"
	// CategoryDependsAdded is an entry added to a dependency array
	// (#476).
	CategoryDependsAdded = "depends_added"
	// CategoryArrayUnreadable is an array the analyzers read that could
	// not be parsed, so no check covered it (#484).
	CategoryArrayUnreadable = "array_unreadable"
)

// Categories lists every category an analyzer can emit.
var Categories = []string{
	CategoryURLNew,
	CategoryPGPRemoved,
	CategoryChecksumRemoved,
	CategoryChecksumRefresh,
	CategoryVersionBump,
	CategoryShellExec,
	CategoryNetworkDownload,
	CategoryInstallChanged,
	CategoryPGPKeyChanged,
	CategoryDependsAdded,
	CategoryArrayUnreadable,
}
