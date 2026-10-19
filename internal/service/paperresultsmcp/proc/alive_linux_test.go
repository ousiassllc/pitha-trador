//go:build linux

package proc_test

import (
	"os"
	"strconv"
	"strings"
)

// pidAlive: the process exists and is not a zombie (a killed child whose
// parent has not reaped it yet is dead for our purposes).
func pidAlive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	return i >= 0 && i+2 < len(s) && s[i+2] != 'Z'
}
