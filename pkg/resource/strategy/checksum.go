package strategy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

func Checksum(o interface{}) (string, error) {
	data, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}
