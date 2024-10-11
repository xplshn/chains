package chains

import (
	"encoding/json"
	"errors"
	"strings"

	_ "embed"
)

// List of all profiles supported by chains out of the box.
// Most of these have only been tested on my (Arch and Nix) systems, so
// they may not work correctly on yours. If that is the case, please report the
// issue and any error messages you encounter so that I can try to fix them
// NOTE: Some app permissions are `aliases` of others, so care must be taken
// that modifying the parent permission will also affect apps based on it
// 105 unique apps currently supported

func FromName(name string) (*AppImagePerms, error) {
	name = strings.ToLower(name)

	profiles := Profiles()

	if p, present := profiles[name]; present {
		p.Files = CleanFiles(p.Files)
		return &p, nil
	}

	return &AppImagePerms{Level: -1}, errors.New("cannot find permissions for app `" + name + "`")
}

//go:embed profile_database.json
var jsonDatabase []byte

var RawProfiles = []AppImagePerms{}

func InitRawProfiles() error {
	if len(RawProfiles) != 0 || len(jsonDatabase) == 0 {
		return nil
	}

	return json.Unmarshal(jsonDatabase, &RawProfiles)
}

func Profiles() map[string]AppImagePerms {
	InitRawProfiles()

	profileMap := make(map[string]AppImagePerms)

	// Add every profile (and its aliases) to the map as a separate value
	for _, profile := range RawProfiles {
		for _, name := range profile.Names {
			profileMap[name] = profile
		}
	}

	return profileMap
}
