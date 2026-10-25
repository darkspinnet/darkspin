//go:build scenario

package main

import "time"

// Explicit screenshot response allowlist; contains no credentials/native handles.
type debugScreenshot struct {
	ClientID       string    `json:"client_id"`
	InstanceID     string    `json:"instance_id"`
	Label          string    `json:"label"`
	Source         string    `json:"source"`
	Sequence       uint32    `json:"sequence"`
	ProcessID      uint32    `json:"process_id"`
	IsAvailable    bool      `json:"is_available"`
	IsComplete     bool      `json:"is_complete"`
	Reason         string    `json:"reason"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	ImagePath      string    `json:"image_path,omitempty"`
	ImageSHA256    string    `json:"image_sha256,omitempty"`
	PixelsSHA256   string    `json:"pixels_sha256,omitempty"`
	MetadataPath   string    `json:"metadata_path,omitempty"`
	MetadataSHA256 string    `json:"metadata_sha256,omitempty"`
	ImageSize      int64     `json:"image_size"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
}
