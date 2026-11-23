package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
)

// ProbeHostInfo identifies the server shared by its independently running clients.
type ProbeHostInfo struct {
	ID       string `json:"host_id"`
	Name     string `json:"host_name"`
	Address  string `json:"host_address,omitempty"`
	Location string `json:"location,omitempty"`
}

// ProbeEnrollment advertises one service/client runtime on a server. Additional
// runtimes enroll with the same server identity and their own runtime identity.
type ProbeEnrollment struct {
	Host          ProbeHostInfo `json:"host"`
	HostSecret    string        `json:"host_secret"`
	RuntimeID     string        `json:"runtime_id"`
	RuntimeSecret string        `json:"runtime_secret"`
	Name          string        `json:"name,omitempty"`
	ServiceType   string        `json:"service_type"`
	ClientType    string        `json:"client_type"`
	Driver        string        `json:"driver,omitempty"`
}

type ProbeEnrollmentReply struct {
	HostID          string `json:"host_id"`
	RuntimeID       string `json:"runtime_id"`
	ObservedAddress string `json:"observed_address"`
}

func DriverService(driver string) (string, string) {
	switch driver {
	case "agora":
		return "rtc", "native"
	case WebDriver:
		return "rtc", "web"
	case "mock":
		return "test", "mock"
	default:
		return "", ""
	}
}

// RuntimeIdentityKey is scoped to a server's private identity and a service/client
// tuple. Recreating a client's data directory cannot create another registration.
func RuntimeIdentityKey(secret, service, client string) (string, string) {
	sign := func(label string) []byte {
		h := hmac.New(sha256.New, []byte(secret))
		h.Write([]byte("probe-v2/" + label + "/" + service + "/" + client))
		return h.Sum(nil)
	}
	return "runtime-" + hex.EncodeToString(sign("identity")[:16]), hex.EncodeToString(sign("credential"))
}

var serviceTypeName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func ValidServiceType(s string) bool { return serviceTypeName.MatchString(s) }
