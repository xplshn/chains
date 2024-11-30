package chains

import (
	"errors"
)

var (
	NilAppImage = errors.New("AppImage is nil")
	NoPath      = errors.New("AppImage contains no path")
	NotMounted  = errors.New("AppImage is not mounted")

	InvalidDesktopFile   = errors.New("desktop file wasn't parsed")
	NoDesktopFile        = errors.New("no (or >1) desktop entry was found inside bundle")
	NoIcon               = errors.New("bundle doesn't specify an icon")
	InvalidIconExtension = errors.New("no valid icon extensions (svg, png) found inside bundle")

	NoMountPoint = errors.New("mount point doesn't exist")
)
