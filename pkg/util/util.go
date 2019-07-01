package util

import (
	"os"
)

const (
	defaultClusterIDVar      = "DEFAULT_CLUSTER_ID"
	fallbackDefaultClusterID = "knative-kinesis-streaming"
)

// GetDefaultClusterID returns the default cluster id to connect with
func GetDefaultClusterID() string {
	return getEnv(defaultClusterIDVar, fallbackDefaultClusterID)
}

func getEnv(envKey string, fallback string) string {
	val, ok := os.LookupEnv(envKey)
	if !ok {
		return fallback
	}
	return val
}
