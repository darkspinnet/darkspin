//go:build scenario

package main

import "time"

// Explicit screenshot response allowlist; contains no credentials/native handles.
type debugScreenshot struct {
	ClientID       string                `json:"client_id"`
	InstanceID     string                `json:"instance_id"`
	Label          string                `json:"label"`
	Source         string                `json:"source"`
	Sequence       uint32                `json:"sequence"`
	ProcessID      uint32                `json:"process_id"`
	IsAvailable    bool                  `json:"is_available"`
	IsComplete     bool                  `json:"is_complete"`
	Reason         string                `json:"reason"`
	StartedAt      time.Time             `json:"started_at"`
	FinishedAt     time.Time             `json:"finished_at"`
	ImagePath      string                `json:"image_path,omitempty"`
	ImageSHA256    string                `json:"image_sha256,omitempty"`
	PixelsSHA256   string                `json:"pixels_sha256,omitempty"`
	MetadataPath   string                `json:"metadata_path,omitempty"`
	MetadataSHA256 string                `json:"metadata_sha256,omitempty"`
	ImageSize      int64                 `json:"image_size"`
	Width          int                   `json:"width"`
	Height         int                   `json:"height"`
	Pixels         debugScreenshotPixels `json:"pixels"`
}

// Image-only observations cannot establish whether a cinematic rendered.
type debugScreenshotPixels struct {
	IsAvailable       bool    `json:"is_available"`
	Reason            string  `json:"reason"`
	Region            string  `json:"region"`
	SampleCount       int     `json:"sample_count"`
	OpaqueFraction    float64 `json:"opaque_fraction"`
	NearBlackFraction float64 `json:"near_black_fraction"`
	MeanChannel       float64 `json:"mean_channel"`
}

func sampleDebugScreenshotPixels(pixels []byte, width, height int) debugScreenshotPixels {
	result := debugScreenshotPixels{Region: "central-half", Reason: "capture pixels unavailable"}
	if width <= 0 || height <= 0 || width > scenarioEntryDimensionLimit || height > scenarioEntryDimensionLimit {
		return result
	}
	if len(pixels) != width*height*4 {
		return result
	}
	left, top := width/4, height/4
	regionWidth, regionHeight := max(1, width/2), max(1, height/2)
	columnCount, rowCount := min(64, regionWidth), min(64, regionHeight)
	var opaqueCount, nearBlackCount, channelSum int
	for row := 0; row < rowCount; row++ {
		y := top + row*regionHeight/rowCount
		for column := 0; column < columnCount; column++ {
			x := left + column*regionWidth/columnCount
			offset := (y*width + x) * 4
			result.SampleCount++
			if pixels[offset+3] != 255 {
				continue
			}
			opaqueCount++
			blue, green, red := int(pixels[offset]), int(pixels[offset+1]), int(pixels[offset+2])
			channelSum += blue + green + red
			if max(blue, green, red) <= 8 {
				nearBlackCount++
			}
		}
	}
	result.OpaqueFraction = float64(opaqueCount) / float64(result.SampleCount)
	if opaqueCount != result.SampleCount {
		result.Reason = "sample contains non-opaque pixels; darkness metrics unavailable"
		return result
	}
	result.IsAvailable = true
	result.Reason = ""
	result.NearBlackFraction = float64(nearBlackCount) / float64(opaqueCount)
	result.MeanChannel = float64(channelSum) / float64(opaqueCount*3)
	return result
}
