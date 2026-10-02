//go:build linux

package termstyle

import "os"

func readPid1Name() (string, error) {
	data, err := os.ReadFile("/proc/1/stat")
	if err != nil {
		return "", err
	}
	return parseStatName(string(data)), nil
}
