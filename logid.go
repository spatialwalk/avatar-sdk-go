package avatarsdkgo

import (
	"fmt"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

const (
	logIDTimeFormat   = "20060102150405"
	logIDNanoIDLength = 12
)

// GenerateLogID returns a log identifier in the format "YYYYMMDDHHMMSS_<nanoid>".
// The timestamp is generated in UTC and the nanoid suffix contains 12 characters.
func GenerateLogID() (string, error) {
	return generateLogID(time.Now().UTC())
}

func generateLogID(now time.Time) (string, error) {
	suffix, err := gonanoid.New(logIDNanoIDLength)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%s", now.Format(logIDTimeFormat), suffix), nil
}
