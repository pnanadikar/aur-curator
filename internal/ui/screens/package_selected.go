package screens

import "github.com/pramod/aur-curator/internal/domain"

// PackageSelectedMsg tells PackageDetailModel which package it is now
// showing. Sent by App.handleNavigate immediately after switching to
// ScreenPackageDetail — PackageDetailModel has no other way to learn
// which package was selected, since Bubble Tea models don't expose
// setters across package boundaries.
type PackageSelectedMsg struct {
	Pkg domain.PackageName
}
