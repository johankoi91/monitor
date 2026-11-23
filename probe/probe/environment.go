package probe

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/johankoi91/monitor/runtime/shared/protocol"
)

func EnvironmentConfig(driver string) (Config, error) {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "RTC_PROBE_") || name == "AVOPS_PROBE_ID" || name == "AVOPS_PROBE_SECRET" {
			return Config{}, errors.New("old probe environment unsupported; configure the v2 server enrollment scheme")
		}
	}
	capacity := 1
	value := os.Getenv("AVOPS_PROBE_CAPACITY")
	auto := value == "auto"
	if value != "" && !auto {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > protocol.MaxWorkerCapacity {
			return Config{}, errors.New("invalid runtime capacity")
		}
		capacity = n
	}
	data := os.Getenv("AVOPS_PROBE_DATA_DIR")
	if data == "" {
		data = "/data"
	}
	host := os.Getenv("AVOPS_PROBE_HOST_DIR")
	if host == "" {
		host = "/host"
	}
	service, client := protocol.DriverService(driver)
	return Config{RuntimeVersion: protocol.ProductVersion, Driver: driver, URL: os.Getenv("AVOPS_PROBE_CENTER_URL"), EnrollmentToken: os.Getenv("AVOPS_PROBE_ENROLL_TOKEN"), HostDir: host, DataDir: data, ServiceType: service, ClientType: client, Name: os.Getenv("AVOPS_PROBE_NAME"), HostName: os.Getenv("AVOPS_PROBE_HOST_NAME"), HostAddress: os.Getenv("AVOPS_PROBE_HOST_ADDRESS"), Location: os.Getenv("AVOPS_PROBE_LOCATION"), Capacity: capacity, AutoCapacity: auto}, nil
}
