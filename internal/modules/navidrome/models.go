package navidrome

import (
	"fmt"
	"time"
)

// SubsonicBase and SubsonicError are openswingsonic's, from
// internal/endpoints/opensubsonicapi/models/base_models.go. ScanStatus is
// new: openswingsonic has no model for it.

// SubsonicBase is what every answer carries.
type SubsonicBase struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	Type          string `json:"type"`
	ServerVersion string `json:"serverVersion"`
	OpenSubsonic  bool   `json:"openSubsonic"`
}

// SubsonicError is a failed answer's error. Its code says what failed, such
// as 40 for a wrong username or password.
type SubsonicError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *SubsonicError) Error() string {
	return fmt.Sprintf("navidrome: error %d: %s", e.Code, e.Message)
}

// ScanStatus is the answer of getScanStatus and startScan.
type ScanStatus struct {
	Scanning    bool       `json:"scanning"`
	Count       int        `json:"count"`
	FolderCount int        `json:"folderCount"`
	LastScan    *time.Time `json:"lastScan"`
}

// envelope is an answer as Navidrome sends it with f=json.
type envelope struct {
	Response struct {
		SubsonicBase
		Error      *SubsonicError `json:"error"`
		ScanStatus *ScanStatus    `json:"scanStatus"`
	} `json:"subsonic-response"`
}
