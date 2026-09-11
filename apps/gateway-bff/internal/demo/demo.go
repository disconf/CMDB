package demo

import "os"

func Enabled() bool {
	return os.Getenv("CMDB_ENABLE_DEMO_DATA") != "false"
}
