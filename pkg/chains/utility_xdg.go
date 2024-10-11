package chains

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
)

// expandEither expands XDG formatted directories into full paths based on the provided map.
func expandEither(str string, xdgDirs map[string]string) string {
	for key, val := range xdgDirs {
		if len(key) > len(str) || key != str[:len(key)] {
			continue
		}

		if key == str[:len(key)] || (len(str) > len(key) && str[len(key)] == '/') {
			str = strings.Replace(str, key, val, 1)
		}
	}

	// Resolve `../` and clean up extra slashes if they exist
	str = filepath.Clean(str)

	// Expand tilde with the true home directory if not generic, otherwise use a generic representation
	if str[0] == '~' {
		str = strings.Replace(str, "~", xdgDirs["xdg-home"], 1)
	}

	// If generic, will fake the home dir. Otherwise does nothing
	str = strings.Replace(str, xdg.Home, xdgDirs["xdg-home"], 1)

	return str
}

// ExpandDir expands XDG and shorthand directories into real directories on the user's machine.
func ExpandDir(str string) string {
	home, present := os.LookupEnv("HOME")
	newHome, _ := RealHome()
	os.Setenv("HOME", newHome)
	xdg.Reload()

	xdgDirs := map[string]string{
		"xdg-home":        xdg.Home,
		"xdg-desktop":     xdg.UserDirs.Desktop,
		"xdg-download":    xdg.UserDirs.Download,
		"xdg-documents":   xdg.UserDirs.Documents,
		"xdg-music":       xdg.UserDirs.Music,
		"xdg-pictures":    xdg.UserDirs.Pictures,
		"xdg-videos":      xdg.UserDirs.Videos,
		"xdg-templates":   xdg.UserDirs.Templates,
		"xdg-publicshare": xdg.UserDirs.PublicShare,
		"xdg-config":      xdg.ConfigHome,
		"xdg-cache":       xdg.CacheHome,
		"xdg-data":        xdg.DataHome,
		"xdg-state":       xdg.StateHome,
	}

	if present {
		os.Setenv("HOME", home)
	}
	xdg.Reload()

	return expandEither(str, xdgDirs)
}

// ExpandGenericDir expands XDG and shorthand directories into generic names to protect actual path names.
func ExpandGenericDir(str string) string {
	home, present := os.LookupEnv("HOME")
	newHome, _ := RealHome()
	os.Setenv("HOME", newHome)
	xdg.Reload()

	xdgDirs := map[string]string{
		"xdg-home":        xdg.Home,
		"xdg-desktop":     xdg.UserDirs.Desktop,
		"xdg-download":    xdg.UserDirs.Download,
		"xdg-documents":   xdg.UserDirs.Documents,
		"xdg-music":       xdg.UserDirs.Music,
		"xdg-pictures":    xdg.UserDirs.Pictures,
		"xdg-videos":      xdg.UserDirs.Videos,
		"xdg-templates":   xdg.UserDirs.Templates,
		"xdg-publicshare": xdg.UserDirs.PublicShare,
		"xdg-config":      xdg.ConfigHome,
		"xdg-cache":       xdg.CacheHome,
		"xdg-data":        xdg.DataHome,
		"xdg-state":       xdg.StateHome,
	}

	if present {
		os.Setenv("HOME", home)
	}
	xdg.Reload()

	return expandEither(str, xdgDirs)
}
