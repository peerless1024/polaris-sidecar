package utils

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/polarismesh/polaris-sidecar/pkg/constants"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
)

// IsFile returns true if the given path is a file, false otherwise.
func IsFile(path string) bool {
	if len(path) == 0 {
		log.Errorf("[utils] path is empty")
		return false
	}
	s, err := os.Stat(path)
	if err != nil {
		log.Errorf("[utils] fail to stat file %s, err: %v", path, err)
		return false
	}
	if s.IsDir() {
		log.Errorf("[utils] %s is not a file", path)
		return false
	}
	return true
}

func ReadFile(path string) ([]byte, error) {
	if !IsFile(path) {
		return nil, nil
	}
	var buf []byte
	var err error
	if buf, err = os.ReadFile(path); err != nil {
		log.Errorf("[utils] fail to read file %s, err: %v", path, err)
		return nil, err
	}
	log.Infof("[config] read file:%s, content:\n%s", path, string(buf))
	return buf, nil
}

// JsonString returns a JSON string representation of the given value.
func JsonString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseLabels parses the given labels string into a map.
// The labels string should be in the format of "key1:value1,key2:value2".
// If the labels string is empty, an empty map is returned.
// If the labels string is invalid, nil is returned.
func ParseLabels(labels string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	values := make(map[string]string)
	tokens := strings.Split(labels, constants.CommaSymbol)
	for _, token := range tokens {
		if len(token) == 0 {
			continue
		}
		pairs := strings.Split(token, constants.ColonSymbol)
		if len(pairs) > 1 {
			values[pairs[0]] = pairs[1]
		}
	}
	return values
}
